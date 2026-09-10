# Stratux NX AHRS algorithms

Select **Old Stratux AHRS** or **Adaptive AHRS v2 (Beta)** in **Settings → AHRS**. Both estimators run continuously. The selected output feeds the shared situation fields used by the web page and ForeFlight/GDL90. Changes here are in the estimator, not a web-only animation filter.

## Old Stratux AHRS

The upstream SimpleAHRS remains the default compatibility mode. It is preserved for comparison and troubleshooting. Credit for its implementation belongs to the Stratux/goflying contributors.

## Adaptive AHRS v2: stability revision

- **Correct Stratux clock handling.** Negative timestamps are legitimate for Stratux's internal clock. They no longer trigger repeated initialization from raw acceleration. Duplicate timestamps do not move the attitude.
- **Time-based filtering.** Gyro filtering uses a 40 ms time constant. Quiet-state acceleration filtering uses 250 ms. These constants follow elapsed time rather than assuming a fixed sampling frequency. Physical sensor vibration and aliasing still require suitable hardware mounting and sensor configuration.
- **Quaternion corrections.** Gyro integration and bounded reference corrections operate in quaternion space. Attitude is not snapped to a single accelerometer reading.
- **Motion-aware reference.** Fresh, plausible GPS horizontal velocity differences supply a horizontal acceleration reference. During motion without this reference, gravity corrections are withheld to avoid pulling a banked aircraft toward level. Vertical acceleration is not compensated; load consistency and innovation gates limit its influence.
- **Conservative initialization.** Initial alignment requires at least one second of quiet, near-1-G measurements. Desktop alignment without GPS is allowed before travel has been observed. Initialization while moving is deliberately withheld.
- **Bias learning with ground evidence.** Runtime gyro-bias learning requires three seconds of quiet IMU measurements and fresh GPS below 2 knots. Losing GPS does not establish that the receiver is stationary. Learned bias is bounded relative to the saved calibration.
- **Fault handling.** Nonfinite values, implausible vectors and time regressions invalidate the sample without overwriting the quaternion. Gaps above 250 ms invalidate alignment. Following observed travel, re-alignment requires ground evidence or an explicit reset/cage. No automatic cage during a turn.
- **Limited unaided operation.** Withheld reference corrections accumulate an unaided timer. At 30 seconds the adaptive attitude becomes invalid, rather than remaining indefinitely marked valid while drifting.
- **Heading semantics.** Heading is marked unavailable until a usable GPS track reference is acquired. GPS track is not true aircraft heading in wind. The track-aided heading remains experimental.
- **Diagnostics.** Logs include filtered gyro/acceleration, correction weight, innovation angle, unaided time, rejected/duplicate sample counts and stream gaps. The displayed quality score is a heuristic, not a calibrated probability or an accuracy guarantee.

## Calibration and operation

The existing `SensorQuaternion`, accelerometer normalization `C`, gyro calibration `D`, orientation, cage and zero-drift controls are shared by both algorithms. Mount the receiver rigidly and align it while stationary. Avoid calibrating while moving. Allow the initial settling interval after selecting/starting the receiver; invalid output during alignment is intentional.

Temporary invalid samples no longer invoke a full adaptive reset in the publication loop. Explicit cage/orientation changes still reset the estimator. The separate upstream AHRS debugging state endpoint continues to expose legacy state; use `/getSituation` and the adaptive CSV diagnostics to inspect the selected output.

## Reproducible software checks

With Go and a C compiler installed, run:

```sh
bash tests/test-ahrs.sh
```

The test harness uses the same pinned goflying version as upstream Stratux (`dd059ec481946361d63dd9c591e2a0efbe4add0a`). It compiles the production estimator and exercises it with the Go race detector. Tests cover negative timestamps, stationary sensor noise, duplicate and corrupt samples, stream gaps, mounting rotation, movement at different sampling rates, sustained coordinated turns, GPS outliers and unaided timeout.

These are deterministic synthetic regressions, not sensor characterization or flight validation. A quiet-noise desktop simulation produced 0.0218 degrees roll/pitch RMS after settling, compared with 0.1686 degrees using the previous implementation under the same inputs. These values must not be interpreted as real-world accuracy specifications.

## Hardware acceptance still required

Record the module identity, mounting, firmware commit and calibration with each comparison. Compare old and new modes at rest, then against known tilt angles and controlled rotations; inspect the actual ForeFlight output as well as the local page. Use raw sensor recordings to reproduce discrepancies. A comparison against Sentry needs synchronized measurements on both devices, including latency, temperature drift and dynamic errors.

This revision remains **Beta**, provides supplemental situational awareness, and has not been demonstrated to match Sentry or certified attitude instruments. Numerical stability alone does not establish aviation reliability. No specific IMU model is assumed, and no sensor register settings are changed by this revision.
