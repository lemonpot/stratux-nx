package main

import (
	"github.com/stratux/goflying/ahrs"
	"math"
	"math/rand"
	"testing"
	"time"
)

func sample(t float64) *ahrs.Measurement { return &ahrs.Measurement{T: t, A3: 1, SValid: true} }
func settled() (*adaptiveAHRS, float64) {
	s := newAdaptiveAHRS()
	tm := -6795364500.0
	for i := 0; i < 100; i++ {
		tm += .05
		s.Compute(sample(tm))
	}
	return s, tm
}
func TestNegativeStratuxClockAndNoise(t *testing.T) {
	s := newAdaptiveAHRS()
	rng := rand.New(rand.NewSource(42))
	tm := float64(time.Time{}.UnixNano()/1000) / 1e6
	var n int
	var sum float64
	for i := 0; i < 1200; i++ {
		tm += .05
		m := sample(tm)
		m.A1 = rng.NormFloat64() * .003
		m.A2 = rng.NormFloat64() * .003
		m.B1 = rng.NormFloat64() * .05
		m.B2 = rng.NormFloat64() * .05
		s.Compute(m)
		if i > 100 {
			if !s.Valid() {
				t.Fatal("invalid while stationary")
			}
			sum += s.roll*s.roll + s.pitch*s.pitch
			n++
		}
	}
	rms := math.Sqrt(sum/float64(2*n)) / ahrs.Deg
	t.Logf("60s desktop, negative Stratux timestamps: roll/pitch RMS %.4f deg", rms)
	if rms > .08 {
		t.Fatalf("excessive desktop jitter: %f", rms)
	}
	if s.stationary {
		t.Fatal("GPS absence allowed stationary bias learning")
	}
	_, _, h := s.RollPitchHeading()
	if h != ahrs.Invalid {
		t.Fatal("invented absolute heading")
	}
}
func TestDuplicateAndInvalidSamples(t *testing.T) {
	s, tm := settled()
	q := s.q
	m := sample(tm)
	m.A2 = .1
	s.Compute(m)
	if s.q != q {
		t.Fatal("duplicate timestamp changed attitude")
	}
	m = sample(tm + .05)
	m.B1 = math.NaN()
	s.Compute(m)
	if s.Valid() || s.q != q {
		t.Fatal("NaN was accepted or corrupted quaternion")
	}
	s.Compute(sample(tm + .10))
	if !s.Valid() {
		t.Fatal("did not recover from a single bad sample")
	}
	m = sample(tm + .15)
	m.A3 = 0
	s.Compute(m)
	if s.Valid() {
		t.Fatal("zero accelerometer accepted")
	}
}
func TestGapDoesNotCageInFlight(t *testing.T) {
	s, tm := settled()
	s.everMoving = true
	q := s.q
	m := sample(tm + 2)
	m.A2 = .4
	s.Compute(m)
	if s.Valid() || s.q != q {
		t.Fatal("gap caged attitude")
	}
	for i := 0; i < 100; i++ {
		s.Compute(sample(tm + 2.05 + float64(i)*.05))
	}
	if s.Valid() {
		t.Fatal("realigned without ground evidence after flight gap")
	}
}
func TestTiltMotionAndMounting(t *testing.T) {
	for _, dt := range []float64{.01, .05, .1} {
		s, tm := settled()
		r := 0.0
		for i := 0; i < int(2/dt); i++ {
			r += 15 * ahrs.Deg * dt
			tm += dt
			m := sample(tm)
			m.B1 = 15
			m.A2 = math.Sin(r)
			m.A3 = math.Cos(r)
			s.Compute(m)
		}
		if math.Abs(s.roll/ahrs.Deg-30) > 1 {
			t.Errorf("dt=%f roll=%f", dt, s.roll/ahrs.Deg)
		}
		for i := 0; i < int(5/dt); i++ {
			tm += dt
			m := sample(tm)
			m.A2 = math.Sin(r)
			m.A3 = math.Cos(r)
			s.Compute(m)
		}
		if math.Abs(s.roll/ahrs.Deg-30) > .3 {
			t.Errorf("settled roll=%f", s.roll/ahrs.Deg)
		}
	}
	s := newAdaptiveAHRS()
	f := [4]float64{math.Sqrt(.5), 0, 0, math.Sqrt(.5)}
	s.SetSensorQuaternion(&f)
	for i := 0; i < 100; i++ {
		m := sample(float64(i) * .05)
		m.A1 = math.Sin(20 * ahrs.Deg)
		m.A3 = math.Cos(20 * ahrs.Deg)
		s.Compute(m)
	}
	if math.Abs(s.roll/ahrs.Deg-20) > .1 {
		t.Fatalf("mount transform wrong: %f", s.roll/ahrs.Deg)
	}
}
func TestCoordinatedTurn(t *testing.T) {
	for _, bankDeg := range []float64{20, 30, 45} {
		s, tm := settled()
		bank := bankDeg * ahrs.Deg
		speed := 100.0
		omega := ahrs.G * math.Tan(bank) / speed
		// Isolate a sustained turn after an ideal roll-in, with physical IMU/GPS data.
		s.roll = bank
		s.headingReferenced = true
		s.everMoving = true
		s.q[0], s.q[1], s.q[2], s.q[3] = ahrs.ToQuaternion(bank, 0, 0)
		heading := 0.0
		for i := 0; i < 600; i++ {
			tm += .05
			heading += omega * .05
			m := sample(tm)
			m.A3 = 1 / math.Cos(bank)
			m.B2 = -omega * math.Sin(bank) / ahrs.Deg
			m.B3 = -omega * math.Cos(bank) / ahrs.Deg
			m.WValid = true
			m.TW = tm
			m.W1 = speed * math.Sin(heading)
			m.W2 = speed * math.Cos(heading)
			s.Compute(m)
		}
		t.Logf("30s coordinated %.0f-deg turn: roll %.3f pitch %.3f", bankDeg, s.roll/ahrs.Deg, s.pitch/ahrs.Deg)
		if !s.Valid() || math.Abs(s.roll-bank) > 3*ahrs.Deg || math.Abs(s.pitch) > 3*ahrs.Deg {
			t.Fatal("turn estimate diverged")
		}
		if math.Abs(s.turnRate-omega/ahrs.Deg) > .3 {
			t.Fatalf("turn rate sign/frame wrong %f", s.turnRate)
		}
	}
}

func TestUnaidedTimeoutAndGPSOutlier(t *testing.T) {
	s, tm := settled()
	s.everMoving = true
	for i := 0; i < 620; i++ {
		tm += .05
		s.Compute(sample(tm))
	}
	if s.Valid() {
		t.Fatal("unaided attitude remained valid indefinitely")
	}
	s, tm = settled()
	m := sample(tm + .05)
	m.WValid = true
	m.TW = m.T
	m.W2 = 100
	s.Compute(m)
	m = sample(tm + .10)
	m.WValid = true
	m.TW = m.T
	m.W2 = 10000
	s.Compute(m)
	if s.gpsAccelReady {
		t.Fatal("GPS velocity jump used as acceleration")
	}
}

func TestPublicationRejectDoesNotReset(t *testing.T) {
	old := globalSettings.AHRSV2_Enabled
	defer func() { globalSettings.AHRSV2_Enabled = old }()
	globalSettings.AHRSV2_Enabled = true
	s := newSelectableAHRS()
	s.adaptive, _ = settled()
	q := s.adaptive.q
	s.adaptive.reject()
	s.ResetInvalid()
	if !s.adaptive.initialized || s.adaptive.q != q {
		t.Fatal("publication reset adaptive state")
	}
	s.Reset()
	if s.adaptive.initialized {
		t.Fatal("explicit cage did not reset")
	}
}
func TestBiasLearningRequiresGPS(t *testing.T) {
	s, tm := settled()
	for i := 0; i < 1200; i++ {
		tm += .05
		m := sample(tm)
		m.B3 = .2
		s.Compute(m)
	}
	if s.gyroBias[2] != 0 {
		t.Fatal("learned bias without GPS")
	}
	for i := 0; i < 1200; i++ {
		tm += .05
		m := sample(tm)
		m.B3 = .2
		m.WValid = true
		m.TW = tm
		s.Compute(m)
	}
	if !s.stationary || s.gyroBias[2] < .15 || s.gyroBias[2] > .21 {
		t.Fatalf("ground bias learning failed %v", s.gyroBias)
	}
}
func TestPitchAndTimestampRegression(t *testing.T) {
	s, tm := settled()
	pitch := 0.0
	for i := 0; i < 40; i++ {
		tm += .05
		pitch += .5 * ahrs.Deg
		m := sample(tm)
		m.B2 = -10
		m.A1 = math.Sin(pitch)
		m.A3 = math.Cos(pitch)
		s.Compute(m)
	}
	if math.Abs(s.pitch/ahrs.Deg-20) > 1 {
		t.Fatalf("pitch direction/response %f", s.pitch/ahrs.Deg)
	}
	q := s.q
	s.Compute(sample(tm - .01))
	if s.Valid() || s.q != q {
		t.Fatal("regressing timestamp accepted")
	}
}
