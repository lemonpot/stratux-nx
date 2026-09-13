package main

// AHRS V2 - TEMPORARILY NOT EXPOSED IN UI
// The code below is preserved and compiles, but the UI selector is hidden.
// AHRSV2_Enabled defaults to false, so selectableAHRS always delegates to
// the upstream Legacy SimpleAHRS. Both algorithms run in parallel; only
// Legacy output is published to mySituation/GDL90/ForeFlight.
// To re-enable the UI selector, set AHRS_V2_UI_VISIBLE = True in
// patch-ahrs-v2.py and rebuild.

import (
	"math"

	"github.com/stratux/goflying/ahrs"
)

// selectableAHRS keeps the upstream Stratux SimpleAHRS running unchanged while
// also running Adaptive AHRS v2. The selected setting controls which solution
// is published to mySituation and therefore to the web UI and ForeFlight/GDL90.
// Keeping both estimators alive makes mode changes immediate and preserves a
// known-good compatibility path during beta testing.
type selectableAHRS struct {
	legacy   *ahrs.SimpleState
	adaptive *adaptiveAHRS
}

func newSelectableAHRS() *selectableAHRS {
	return &selectableAHRS{
		legacy:   ahrs.NewSimpleAHRS(),
		adaptive: newAdaptiveAHRS(),
	}
}

func (s *selectableAHRS) useAdaptive() bool {
	return globalSettings.AHRSV2_Enabled
}

func (s *selectableAHRS) SetCalibrations(c, d *[3]float64) {
	s.legacy.SetCalibrations(c, d)
	s.adaptive.SetCalibrations(c, d)
}

func (s *selectableAHRS) SetSensorQuaternion(f *[4]float64) {
	s.legacy.SetSensorQuaternion(f)
	s.adaptive.SetSensorQuaternion(f)
}

func (s *selectableAHRS) Reset() {
	s.legacy.Reset()
	s.adaptive.Reset()
}

func (s *selectableAHRS) Compute(m *ahrs.Measurement) {
	// Always run both. This is deliberate: switching algorithms in Settings is
	// immediate and does not require a reboot or a cold-start convergence period.
	s.legacy.Compute(m)
	s.adaptive.Compute(m)
}

func (s *selectableAHRS) Valid() bool {
	if s.useAdaptive() {
		return s.adaptive.Valid()
	}
	return s.legacy.Valid()
}

func (s *selectableAHRS) RollPitchHeading() (float64, float64, float64) {
	if s.useAdaptive() {
		return s.adaptive.RollPitchHeading()
	}
	return s.legacy.RollPitchHeading()
}

func (s *selectableAHRS) SlipSkid() float64 {
	if s.useAdaptive() {
		return s.adaptive.SlipSkid()
	}
	return s.legacy.SlipSkid()
}

func (s *selectableAHRS) RateOfTurn() float64 {
	if s.useAdaptive() {
		return s.adaptive.RateOfTurn()
	}
	return s.legacy.RateOfTurn()
}

func (s *selectableAHRS) GLoad() float64 {
	if s.useAdaptive() {
		return s.adaptive.GLoad()
	}
	return s.legacy.GLoad()
}

// The upstream AHRS web listener expects the full legacy State object. Keep
// feeding it that state for compatibility. Adaptive-specific diagnostics are
// exposed through GetLogMap and /getSituation instead.
func (s *selectableAHRS) GetState() *ahrs.State {
	return s.legacy.GetState()
}

func (s *selectableAHRS) GetLogMap() map[string]interface{} {
	if s.useAdaptive() {
		return s.adaptive.GetLogMap()
	}
	return s.legacy.GetLogMap()
}

func (s *selectableAHRS) AlgorithmName() string {
	if s.useAdaptive() {
		return "Adaptive AHRS v2 (Beta)"
	}
	return "Old Stratux AHRS (Legacy)"
}

func (s *selectableAHRS) Confidence() float64 {
	if s.useAdaptive() {
		return s.adaptive.Confidence()
	}
	if s.legacy.Valid() {
		return 1
	}
	return 0
}

func (s *selectableAHRS) Stationary() bool {
	return s.useAdaptive() && s.adaptive.Stationary()
}

// ResetInvalid preserves the adaptive state across rejected samples. A bad
// packet must not cage the aircraft to its instantaneous apparent gravity.
func (s *selectableAHRS) ResetInvalid() {
	if !s.useAdaptive() {
		s.legacy.Reset()
	}
}

// adaptiveAHRS uses body-to-earth quaternions (X nose, Y left, Z up).
// The quality score is a diagnostic heuristic, not an accuracy probability.
type adaptiveAHRS struct {
	q, f                                                      [4]float64
	gyroBias, savedBias                                       [3]float64
	accel, gyro                                               [3]float64
	gpsVelocity, gpsAcceleration                              [3]float64
	aNorm, lastT, lastTW, gpsAccelT                           float64
	initialized, valid, filtersReady, gpsReady, gpsAccelReady bool
	everMoving, stationary, headingReferenced                 bool
	settleFor, stationaryFor, unaidedFor                      float64
	confidence, accelConfidence, lastAccelInnovation          float64
	roll, pitch, heading, slipSkid, turnRate, gLoad           float64
	rejected, duplicates, gaps                                float64
	logMap                                                    map[string]interface{}
}

func newAdaptiveAHRS() *adaptiveAHRS {
	s := &adaptiveAHRS{f: [4]float64{1, 0, 0, 0}, aNorm: 1}
	s.Reset()
	return s
}
func finiteAHRS(v float64) bool           { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func clampAHRS(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
func wrapAHRSAngle(v float64) float64     { return math.Atan2(math.Sin(v), math.Cos(v)) }
func normAHRS(v [3]float64) float64       { return math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2]) }
func finiteVectorAHRS(v [3]float64) bool {
	return finiteAHRS(v[0]) && finiteAHRS(v[1]) && finiteAHRS(v[2])
}
func gainAHRS(dt, tau float64) float64 { return -math.Expm1(-dt / tau) }
func filterAHRS(dst *[3]float64, src [3]float64, k float64) {
	for i := range dst {
		dst[i] += k * (src[i] - dst[i])
	}
}
func rotateAHRS(q [4]float64, v [3]float64, inverse bool) [3]float64 {
	if inverse {
		q[1], q[2], q[3] = -q[1], -q[2], -q[3]
	}
	r := ahrs.QuaternionToRotationMatrix(q[0], q[1], q[2], q[3])
	return [3]float64{r[0][0]*v[0] + r[0][1]*v[1] + r[0][2]*v[2], r[1][0]*v[0] + r[1][1]*v[1] + r[1][2]*v[2], r[2][0]*v[0] + r[2][1]*v[1] + r[2][2]*v[2]}
}
func (s *adaptiveAHRS) SetCalibrations(c, d *[3]float64) {
	if c != nil && finiteVectorAHRS(*c) {
		n := normAHRS(*c)
		if n > .5 && n < 1.5 {
			s.aNorm = n
		}
	}
	if d != nil && finiteVectorAHRS(*d) && *d != s.savedBias {
		s.savedBias = *d
		s.gyroBias = *d
	}
}
func (s *adaptiveAHRS) SetSensorQuaternion(f *[4]float64) {
	if f == nil {
		return
	}
	n := math.Sqrt(f[0]*f[0] + f[1]*f[1] + f[2]*f[2] + f[3]*f[3])
	if !finiteAHRS(n) || n < .5 {
		return
	}
	next := [4]float64{f[0] / n, f[1] / n, f[2] / n, f[3] / n}
	dot := 0.0
	for i := range next {
		dot += next[i] * s.f[i]
	}
	if math.Abs(dot) < 1-1e-10 {
		s.f = next
		s.Reset()
	}
}
func gpsHeading(m *ahrs.Measurement) (float64, bool) {
	if !m.WValid || !finiteAHRS(m.W1) || !finiteAHRS(m.W2) || !finiteAHRS(m.TW) || m.T-m.TW < -.1 || m.T-m.TW > 1.5 || math.Hypot(m.W1, m.W2) < 8 {
		return 0, false
	}
	h := math.Atan2(m.W1, m.W2)
	if h < 0 {
		h += 2 * math.Pi
	}
	return h, true
}
func accelAttitude(a1, a2, a3 float64) (float64, float64) {
	return math.Atan2(-a2, -a3), math.Atan2(-a1, math.Hypot(a2, a3))
}
func (s *adaptiveAHRS) reject() {
	s.valid = false
	s.confidence = 0
	s.stationary = false
	s.stationaryFor = 0
	s.settleFor = 0
	s.rejected++
	s.logDiagnostics()
}

// GPS horizontal acceleration is used only with fresh, plausible consecutive
// fixes. Vertical velocity is intentionally excluded: upstream may mix baro
// and GPS sources. The reference is apparent gravity [-dvE/g,-dvN/g,-1].
func (s *adaptiveAHRS) updateGPS(m *ahrs.Measurement) bool {
	fresh := m.WValid && finiteAHRS(m.TW) && finiteAHRS(m.W1) && finiteAHRS(m.W2) && m.T-m.TW >= -.1 && m.T-m.TW <= 1.5
	if !fresh {
		s.gpsReady = false
		s.gpsAccelReady = false
		return false
	}
	if math.Hypot(m.W1, m.W2) > 8 {
		s.everMoving = true
	}
	if !s.gpsReady {
		s.gpsVelocity = [3]float64{m.W1, m.W2, 0}
		s.lastTW = m.TW
		s.gpsReady = true
		return true
	}
	dt := m.TW - s.lastTW
	if dt == 0 {
		return true
	}
	if dt < .05 || dt > 1.5 {
		s.gpsAccelReady = false
	} else {
		a := [3]float64{(m.W1 - s.gpsVelocity[0]) / dt / ahrs.G, (m.W2 - s.gpsVelocity[1]) / dt / ahrs.G, 0}
		if normAHRS(a) < 2.0 {
			if !s.gpsAccelReady {
				s.gpsAcceleration = a
			} else {
				filterAHRS(&s.gpsAcceleration, a, gainAHRS(dt, .5))
			}
			s.gpsAccelReady = true
			s.gpsAccelT = m.TW
		} else {
			s.gpsAccelReady = false
		}
	}
	s.lastTW = m.TW
	s.gpsVelocity = [3]float64{m.W1, m.W2, 0}
	return true
}
func (s *adaptiveAHRS) Compute(m *ahrs.Measurement) {
	if m == nil || !m.SValid || !finiteAHRS(m.T) {
		s.reject()
		return
	}
	rawA := [3]float64{-m.A1, -m.A2, -m.A3}
	rawB := [3]float64{m.B1 - s.gyroBias[0], m.B2 - s.gyroBias[1], m.B3 - s.gyroBias[2]}
	if !finiteVectorAHRS(rawA) || !finiteVectorAHRS(rawB) || normAHRS(rawA) < .05 || normAHRS(rawA) > 6 || normAHRS(rawB) > 500 {
		s.reject()
		return
	}
	a := rotateAHRS(s.f, rawA, false)
	b := rotateAHRS(s.f, rawB, false)
	dt := .05
	if s.filtersReady {
		dt = m.T - s.lastT
		if dt <= 0 {
			s.duplicates++
			s.logDiagnostics()
			if dt < 0 {
				s.reject()
			}
			return
		}
		if dt > .25 {
			s.lastT = m.T
			s.filtersReady = false
			s.gpsReady = false
			s.gpsAccelReady = false
			s.initialized = false
			s.gaps++
			s.reject()
			return
		}
	}
	s.lastT = m.T
	if !s.filtersReady {
		s.accel = a
		s.gyro = b
		s.filtersReady = true
	} else {
		filterAHRS(&s.accel, a, gainAHRS(dt, .25))
		filterAHRS(&s.gyro, b, gainAHRS(dt, .04))
	}
	gpsFresh := s.updateGPS(m)
	gs := 0.0
	if gpsFresh {
		gs = math.Hypot(m.W1, m.W2)
	}
	amag := normAHRS(a) / s.aNorm
	noise := normAHRS([3]float64{a[0] - s.accel[0], a[1] - s.accel[1], a[2] - s.accel[2]}) / s.aNorm
	quiet := math.Abs(amag-1) < .035 && normAHRS(b) < .8 && noise < .025
	// Missing GPS is not proof of being parked. Bias learning requires GPS
	// corroboration; desktop alignment is allowed only before observed travel.
	grounded := gpsFresh && gs < 2
	alignable := quiet && (grounded || !s.everMoving && !gpsFresh)
	if !s.initialized {
		if alignable {
			s.settleFor += dt
		} else {
			s.settleFor = 0
		}
		if s.settleFor < 1.0 {
			s.valid = false
			s.confidence = 0
			s.logDiagnostics()
			return
		}
		s.roll, s.pitch = accelAttitude(s.accel[0], s.accel[1], s.accel[2])
		s.heading = 0
		s.headingReferenced = false
		s.q[0], s.q[1], s.q[2], s.q[3] = ahrs.ToQuaternion(s.roll, s.pitch, s.heading)
		s.initialized = true
	}
	if quiet && grounded {
		s.stationaryFor += dt
	} else {
		s.stationaryFor = 0
	}
	s.stationary = s.stationaryFor >= 3
	if s.stationary {
		k := gainAHRS(dt, 30)
		raw := [3]float64{m.B1, m.B2, m.B3}
		for i := range raw {
			s.gyroBias[i] = clampAHRS(s.gyroBias[i]+k*(raw[i]-s.gyroBias[i]), s.savedBias[i]-2, s.savedBias[i]+2)
		}
	}
	// Integrate a bounded timestep; filtering is independent of sample rate.
	s.q[0], s.q[1], s.q[2], s.q[3] = ahrs.QuaternionRotate(s.q[0], s.q[1], s.q[2], s.q[3], s.gyro[0]*dt*ahrs.Deg, s.gyro[1]*dt*ahrs.Deg, s.gyro[2]*dt*ahrs.Deg)
	s.roll, s.pitch, s.heading = ahrs.FromQuaternion(s.q[0], s.q[1], s.q[2], s.q[3])
	h, hOK := gpsHeading(m)
	if hOK && gs >= 20 {
		if !s.headingReferenced {
			s.heading = h
			s.headingReferenced = true
		} else {
			s.heading += gainAHRS(dt, 60) * wrapAHRSAngle(h-s.heading)
		}
		s.q[0], s.q[1], s.q[2], s.q[3] = ahrs.ToQuaternion(s.roll, s.pitch, s.heading)
	}
	reference := [3]float64{0, 0, -1}
	compensated := gpsFresh && s.gpsAccelReady && m.T-s.gpsAccelT <= 1.0 && s.headingReferenced
	if compensated {
		reference[0] = -s.gpsAcceleration[0]
		reference[1] = -s.gpsAcceleration[1]
	}
	expected := rotateAHRS(s.q, reference, true)
	// Do not filter the moving gravity direction during maneuvers. Only the
	// quiet desktop/ground path uses the slower acceleration filter.
	measured := a
	if alignable {
		measured = s.accel
	}
	en, mn := normAHRS(expected), normAHRS(measured)
	dot := 0.0
	for i := range expected {
		expected[i] /= en
		measured[i] /= mn
		dot += expected[i] * measured[i]
	}
	s.lastAccelInnovation = math.Acos(clampAHRS(dot, -1, 1)) / ahrs.Deg
	s.accelConfidence = clampAHRS(1-math.Abs(amag-en)/.12, 0, 1) * clampAHRS(1-noise/.15, 0, 1)
	if s.everMoving && !compensated && !grounded {
		s.accelConfidence = 0
	}
	if !compensated && !alignable && (normAHRS(b) > .5 || math.Abs(s.roll) > 5*ahrs.Deg) {
		s.accelConfidence = 0
	}
	if s.lastAccelInnovation > 20 {
		s.accelConfidence = 0
	}
	if s.accelConfidence > .2 {
		s.unaidedFor = 0
	} else {
		s.unaidedFor += dt
	}
	err := [3]float64{measured[1]*expected[2] - measured[2]*expected[1], measured[2]*expected[0] - measured[0]*expected[2], measured[0]*expected[1] - measured[1]*expected[0]}
	tau := 8.0
	if alignable {
		tau = 1.5
	}
	k := gainAHRS(dt, tau) * s.accelConfidence
	s.q[0], s.q[1], s.q[2], s.q[3] = ahrs.QuaternionRotate(s.q[0], s.q[1], s.q[2], s.q[3], k*err[0], k*err[1], k*err[2])
	s.roll, s.pitch, s.heading = ahrs.FromQuaternion(s.q[0], s.q[1], s.q[2], s.q[3])
	k = gainAHRS(dt, .35)
	s.gLoad += k * (-a[2]/s.aNorm - s.gLoad)
	slip := math.Atan2(a[1], -a[2]) / ahrs.Deg
	s.slipSkid += k * wrapAHRSAngle((slip-s.slipSkid)*ahrs.Deg) / ahrs.Deg
	// Navigation yaw rate, not the body Z gyro. Heading increases clockwise.
	if math.Abs(math.Cos(s.pitch)) > .1 {
		rate := (-s.gyro[1]*math.Sin(s.roll) - s.gyro[2]*math.Cos(s.roll)) / math.Cos(s.pitch)
		s.turnRate += gainAHRS(dt, .2) * (rate - s.turnRate)
	} else {
		s.turnRate = ahrs.Invalid
	}
	s.confidence = clampAHRS((.5+.4*s.accelConfidence)*(1-s.unaidedFor/30), 0, 1)
	s.valid = s.unaidedFor < 30 && finiteAHRS(s.roll) && finiteAHRS(s.pitch) && finiteAHRS(s.heading)
	if !s.valid {
		s.confidence = 0
	}
	s.logDiagnostics()
}
func (s *adaptiveAHRS) logDiagnostics() {
	s.logMap["AHRSAlgorithmV2"] = 1.0
	s.logMap["AHRSConfidence"] = s.confidence
	s.logMap["AHRSStationary"] = 0.0
	if s.stationary {
		s.logMap["AHRSStationary"] = 1.0
	}
	s.logMap["T"] = s.lastT
	s.logMap["Roll"] = s.roll / ahrs.Deg
	s.logMap["Pitch"] = s.pitch / ahrs.Deg
	s.logMap["Heading"] = s.heading / ahrs.Deg
	s.logMap["AccelConfidence"] = s.accelConfidence
	s.logMap["AccelInnovationDeg"] = s.lastAccelInnovation
	s.logMap["UnaidedSeconds"] = s.unaidedFor
	s.logMap["RejectedSamples"] = s.rejected
	s.logMap["DuplicateSamples"] = s.duplicates
	s.logMap["StreamGaps"] = s.gaps
	s.logMap["GyroX"] = s.gyro[0]
	s.logMap["GyroY"] = s.gyro[1]
	s.logMap["GyroZ"] = s.gyro[2]
	s.logMap["AccelX"] = s.accel[0]
	s.logMap["AccelY"] = s.accel[1]
	s.logMap["AccelZ"] = s.accel[2]
	s.logMap["GLoad"] = s.gLoad
}
func (s *adaptiveAHRS) Valid() bool { return s.valid }
func (s *adaptiveAHRS) RollPitchHeading() (float64, float64, float64) {
	if !s.valid {
		return ahrs.Invalid, ahrs.Invalid, ahrs.Invalid
	}
	h := ahrs.Invalid
	if s.headingReferenced {
		h = s.heading
	}
	return s.roll, s.pitch, h
}
func (s *adaptiveAHRS) SlipSkid() float64 {
	if !s.valid {
		return ahrs.Invalid
	}
	return s.slipSkid
}
func (s *adaptiveAHRS) RateOfTurn() float64 {
	if !s.valid {
		return ahrs.Invalid
	}
	return s.turnRate
}
func (s *adaptiveAHRS) GLoad() float64 {
	if !s.valid {
		return ahrs.Invalid
	}
	return s.gLoad
}
func (s *adaptiveAHRS) Reset() {
	f, n, b, d := s.f, s.aNorm, s.gyroBias, s.savedBias
	// Preserve the map identity held by the upstream CSV logger.
	logs := s.logMap
	if logs == nil {
		logs = make(map[string]interface{})
	}
	*s = adaptiveAHRS{f: f, aNorm: n, gyroBias: b, savedBias: d, q: [4]float64{1, 0, 0, 0}, gLoad: 1, logMap: logs}
	s.logDiagnostics()
}
func (s *adaptiveAHRS) GetLogMap() map[string]interface{} { return s.logMap }
func (s *adaptiveAHRS) Confidence() float64               { return s.confidence }
func (s *adaptiveAHRS) Stationary() bool                  { return s.stationary }
