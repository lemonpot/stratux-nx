package main

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

// adaptiveAHRS is intentionally smaller than the experimental full EKF in
// goflying. It uses quaternion gyro propagation for fast response, then applies
// bounded low-frequency attitude corrections whose strength depends on whether
// the accelerometer is actually behaving like a gravity reference. It also
// learns gyro bias while stationary and refuses implausible sensor solutions.
type adaptiveAHRS struct {
	q                  [4]float64
	f                  [4]float64
	accelCal           [3]float64
	gyroBias           [3]float64
	aNorm               float64
	lastT               float64
	lastTW              float64
	initialized         bool
	valid               bool
	confidence          float64
	stationaryFor       float64
	stationary          bool
	accelConfidence     float64
	lastAccelInnovation float64
	roll                float64
	pitch               float64
	heading              float64
	slipSkid             float64
	turnRate             float64
	gLoad               float64
	gyroAircraft        [3]float64
	logMap               map[string]interface{}
}

func newAdaptiveAHRS() *adaptiveAHRS {
	s := &adaptiveAHRS{
		f:      [4]float64{1, 0, 0, 0},
		aNorm:  1,
		logMap: make(map[string]interface{}),
	}
	s.Reset()
	return s
}

func clampAHRS(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func wrapAHRSAngle(v float64) float64 {
	for v > math.Pi {
		v -= 2 * math.Pi
	}
	for v < -math.Pi {
		v += 2 * math.Pi
	}
	return v
}

func finiteAHRS(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func (s *adaptiveAHRS) SetCalibrations(c, d *[3]float64) {
	if c != nil {
		s.accelCal = *c
		n := math.Sqrt(c[0]*c[0] + c[1]*c[1] + c[2]*c[2])
		if n > 0.5 && n < 1.5 {
			s.aNorm = n
		} else {
			s.aNorm = 1
		}
	}
	if d != nil {
		s.gyroBias = *d
	}
}

func (s *adaptiveAHRS) SetSensorQuaternion(f *[4]float64) {
	if f == nil {
		return
	}
	n := math.Sqrt(f[0]*f[0] + f[1]*f[1] + f[2]*f[2] + f[3]*f[3])
	if n < 0.5 || !finiteAHRS(n) {
		s.f = [4]float64{1, 0, 0, 0}
		return
	}
	s.f = [4]float64{f[0] / n, f[1] / n, f[2] / n, f[3] / n}
}

// rotateSensorToAircraft mirrors goflying State.rotateByF(). SensorQuaternion
// is therefore interpreted exactly the same way by both legacy and v2 modes.
func (s *adaptiveAHRS) rotateSensorToAircraft(x, y, z float64) (float64, float64, float64) {
	f0, f1, f2, f3 := s.f[0], s.f[1], s.f[2], s.f[3]
	f11 := f0*f0 + f1*f1 - f2*f2 - f3*f3
	f12 := 2 * (-f0*f3 + f1*f2)
	f13 := 2 * (f0*f2 + f3*f1)
	f21 := 2 * (f0*f3 + f1*f2)
	f22 := f0*f0 - f1*f1 + f2*f2 - f3*f3
	f23 := 2 * (-f0*f1 + f2*f3)
	f31 := 2 * (-f0*f2 + f3*f1)
	f32 := 2 * (f0*f1 + f2*f3)
	f33 := f0*f0 - f1*f1 - f2*f2 + f3*f3
	return f11*x + f12*y + f13*z,
		f21*x + f22*y + f23*z,
		f31*x + f32*y + f33*z
}

// accelAttitude returns roll/pitch using the gravity vector in Stratux's
// aircraft frame: X nose, Y left wing, Z up. A level aircraft is approximately
// [0,0,-1] after the same sign convention used by SimpleAHRS.
func accelAttitude(a1, a2, a3 float64) (roll, pitch float64) {
	roll = math.Atan2(-a2, -a3)
	pitch = math.Atan2(-a1, math.Sqrt(a2*a2+a3*a3))
	return
}

func gpsHeading(m *ahrs.Measurement) (float64, bool) {
	if !m.WValid {
		return 0, false
	}
	gs := math.Hypot(m.W1, m.W2)
	if gs < 8 {
		return 0, false
	}
	h := math.Atan2(m.W1, m.W2)
	if h < 0 {
		h += 2 * math.Pi
	}
	return h, true
}

func (s *adaptiveAHRS) initializeFromSensors(m *ahrs.Measurement, a1, a2, a3 float64) {
	roll, pitch := accelAttitude(a1, a2, a3)
	heading := s.heading
	if !s.initialized || !finiteAHRS(heading) {
		heading = 0
	}
	if h, ok := gpsHeading(m); ok {
		heading = h
	}
	s.q[0], s.q[1], s.q[2], s.q[3] = ahrs.ToQuaternion(roll, pitch, heading)
	s.roll, s.pitch, s.heading = roll, pitch, heading
	s.initialized = true
	s.valid = true
	s.lastT = m.T
	s.lastTW = m.TW
}

func (s *adaptiveAHRS) Compute(m *ahrs.Measurement) {
	if m == nil || !m.SValid {
		s.valid = false
		s.confidence *= 0.8
		return
	}

	// Match the legacy sign convention for acceleration and calibration frame.
	a1, a2, a3 := s.rotateSensorToAircraft(-m.A1, -m.A2, -m.A3)
	gx, gy, gz := s.rotateSensorToAircraft(
		m.B1-s.gyroBias[0],
		m.B2-s.gyroBias[1],
		m.B3-s.gyroBias[2],
	)
	s.gyroAircraft = [3]float64{gx, gy, gz}

	amag := math.Sqrt(a1*a1 + a2*a2 + a3*a3)
	if s.aNorm <= 0.5 {
		s.aNorm = 1
	}
	gRatio := amag / s.aNorm
	// Full trust close to 1 G, then smoothly reduce trust. At 1.25 G (or
	// 0.75 G) the accelerometer contributes no attitude correction.
	s.accelConfidence = clampAHRS(1-math.Abs(gRatio-1)/0.25, 0, 1)

	gyroMag := math.Sqrt(gx*gx + gy*gy + gz*gz)
	gs := 0.0
	if m.WValid {
		gs = math.Hypot(m.W1, m.W2)
	}

	if !s.initialized || s.lastT <= 0 {
		s.initializeFromSensors(m, a1, a2, a3)
		s.updateDerived(a1, a2, a3, gx, gy, gz, gs, false)
		return
	}

	dt := m.T - s.lastT
	if dt <= 0 || dt > 1.0 || !finiteAHRS(dt) {
		// A stale/restarted sensor stream should converge from gravity instead of
		// integrating a huge time step and potentially flipping the horizon.
		s.initializeFromSensors(m, a1, a2, a3)
		s.updateDerived(a1, a2, a3, gx, gy, gz, gs, false)
		return
	}

	stationaryCandidate := s.accelConfidence > 0.78 && gyroMag < 1.2 && (!m.WValid || gs < 2.0)
	if stationaryCandidate {
		s.stationaryFor += dt
	} else {
		s.stationaryFor = 0
	}
	s.stationary = s.stationaryFor >= 2.0

	// Learn residual gyro bias only when multiple independent cues agree that
	// the aircraft is stationary. This is intentionally slow and never writes
	// flash continuously; it is a runtime correction layered on saved D bias.
	if s.stationary {
		k := clampAHRS(dt*0.035, 0, 0.01)
		s.gyroBias[0] += k * (m.B1 - s.gyroBias[0])
		s.gyroBias[1] += k * (m.B2 - s.gyroBias[1])
		s.gyroBias[2] += k * (m.B3 - s.gyroBias[2])
	}

	// Fast path: gyro propagation in quaternion space. This avoids Euler-angle
	// singularities and provides the low-latency response needed in turbulence.
	if gyroMag < 720 && finiteAHRS(gx) && finiteAHRS(gy) && finiteAHRS(gz) {
		s.q[0], s.q[1], s.q[2], s.q[3] = ahrs.QuaternionRotate(
			s.q[0], s.q[1], s.q[2], s.q[3],
			gx*dt*ahrs.Deg, gy*dt*ahrs.Deg, gz*dt*ahrs.Deg,
		)
	} else {
		s.valid = false
		s.confidence = 0
		s.lastT = m.T
		return
	}

	roll, pitch, heading := ahrs.FromQuaternion(s.q[0], s.q[1], s.q[2], s.q[3])
	accRoll, accPitch := accelAttitude(a1, a2, a3)
	rollErr := wrapAHRSAngle(accRoll - roll)
	pitchErr := wrapAHRSAngle(accPitch - pitch)
	s.lastAccelInnovation = math.Sqrt(rollErr*rollErr + pitchErr*pitchErr) / ahrs.Deg

	// If we are definitely stationary and the propagated attitude disagrees
	// wildly with gravity, recover immediately. This is the specific class of
	// failure that can otherwise leave a parked receiver showing ~180 degrees.
	hardRecovery := s.stationary && s.accelConfidence > 0.9 &&
		(math.Abs(rollErr) > 35*ahrs.Deg || math.Abs(pitchErr) > 25*ahrs.Deg)
	if hardRecovery {
		roll = accRoll
		pitch = accPitch
	} else if s.accelConfidence > 0 {
		// Adaptive low-frequency correction. In flight the accelerometer gets a
		// gentle vote; while stationary gravity becomes a strong reference.
		gainPerSecond := 0.20
		if s.stationary {
			gainPerSecond = 1.6
		}
		gain := clampAHRS(dt*gainPerSecond*s.accelConfidence, 0, 0.12)
		// Innovation gate: never let one acceleration transient drag the horizon.
		if math.Abs(rollErr) < 35*ahrs.Deg && math.Abs(pitchErr) < 25*ahrs.Deg {
			roll += gain * rollErr
			pitch += gain * pitchErr
		}
	}

	// GPS track is not aircraft heading in wind, so v2 only uses it as a very
	// slow yaw-drift constraint once moving fast enough. It cannot drive roll or
	// pitch and it is ignored at taxi/parking speeds.
	if h, ok := gpsHeading(m); ok && gs >= 20 {
		hErr := wrapAHRSAngle(h - heading)
		if math.Abs(hErr) < 70*ahrs.Deg {
			heading += clampAHRS(dt*0.015, 0, 0.003) * hErr
		}
	}

	roll, pitch, heading = ahrs.Regularize(roll, pitch, heading)
	s.q[0], s.q[1], s.q[2], s.q[3] = ahrs.ToQuaternion(roll, pitch, heading)
	s.roll, s.pitch, s.heading = roll, pitch, heading
	s.lastT = m.T
	s.lastTW = m.TW
	s.updateDerived(a1, a2, a3, gx, gy, gz, gs, hardRecovery)
}

func (s *adaptiveAHRS) updateDerived(a1, a2, a3, gx, gy, gz, gs float64, recovered bool) {
	if s.aNorm <= 0.5 {
		s.aNorm = 1
	}
	instantG := -a3 / s.aNorm
	if !finiteAHRS(s.gLoad) || s.gLoad == 0 {
		s.gLoad = instantG
	} else {
		s.gLoad += 0.12 * (instantG - s.gLoad)
	}
	instantSlip := math.Atan2(a2, -a3) / ahrs.Deg
	if !finiteAHRS(s.slipSkid) {
		s.slipSkid = instantSlip
	} else {
		s.slipSkid += 0.12 * (instantSlip - s.slipSkid)
	}
	if !finiteAHRS(s.turnRate) {
		s.turnRate = gz
	} else {
		s.turnRate += 0.20 * (gz - s.turnRate)
	}

	conf := 0.42 + 0.38*s.accelConfidence
	if gs >= 8 {
		conf += 0.08
	}
	if s.stationary {
		conf += 0.12
	}
	if s.lastAccelInnovation > 30 {
		conf -= 0.18
	}
	if recovered {
		conf -= 0.08
	}
	s.confidence = clampAHRS(conf, 0, 1)
	s.valid = s.initialized && s.confidence >= 0.40 &&
		finiteAHRS(s.roll) && finiteAHRS(s.pitch) && finiteAHRS(s.heading) &&
		finiteAHRS(s.gLoad) && math.Abs(s.pitch) <= 90*ahrs.Deg

	s.logMap["AHRSAlgorithm"] = "Adaptive AHRS v2 (Beta)"
	s.logMap["AHRSConfidence"] = s.confidence
	s.logMap["AHRSStationary"] = s.stationary
	s.logMap["AccelConfidence"] = s.accelConfidence
	s.logMap["AccelInnovationDeg"] = s.lastAccelInnovation
	s.logMap["Roll"] = s.roll / ahrs.Deg
	s.logMap["Pitch"] = s.pitch / ahrs.Deg
	s.logMap["Heading"] = s.heading / ahrs.Deg
	s.logMap["GyroX"] = gx
	s.logMap["GyroY"] = gy
	s.logMap["GyroZ"] = gz
	s.logMap["GroundSpeed"] = gs
	s.logMap["GLoad"] = s.gLoad
}

func (s *adaptiveAHRS) Valid() bool { return s.valid }

func (s *adaptiveAHRS) RollPitchHeading() (float64, float64, float64) {
	if !s.valid {
		return ahrs.Invalid, ahrs.Invalid, ahrs.Invalid
	}
	heading := s.heading
	// Like legacy Stratux, heading without a useful movement reference should
	// not be presented as an absolute heading.
	return s.roll, s.pitch, heading
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
	s.initialized = false
	s.valid = false
	s.confidence = 0
	s.stationaryFor = 0
	s.stationary = false
	s.lastT = 0
	s.lastTW = 0
	s.lastAccelInnovation = 0
	s.q = [4]float64{1, 0, 0, 0}
}

func (s *adaptiveAHRS) GetLogMap() map[string]interface{} { return s.logMap }
func (s *adaptiveAHRS) Confidence() float64              { return s.confidence }
func (s *adaptiveAHRS) Stationary() bool                 { return s.stationary }
