# Stratux AHRS algorithms

This custom Stratux build provides two selectable AHRS algorithms. The selection is stored in `stratux.conf` as `AHRSV2_Enabled` and can be changed from **Settings → AHRS** without rebooting.

## Old Stratux AHRS (Legacy)

This is the upstream Stratux `SimpleAHRS` implementation, preserved unchanged as the compatibility option. It fuses gyro, accelerometer and GPS-derived motion with fixed smoothing constants and a fixed GPS fusion weight. The upstream implementation is intentionally simple and its `Valid()` method currently always returns `true` after computation, so it has no independent confidence score or strong plausibility gate for an attitude solution.

Use Legacy when comparing behavior with stock Stratux or when troubleshooting compatibility.

## Adaptive AHRS v2 (Beta)

Adaptive AHRS v2 is the new optional estimator in this build. It runs in parallel with Legacy, but only the selected algorithm is published to `mySituation`, the Stratux AHRS page and AHRS-capable GDL90 outputs such as ForeFlight.

The v2 design is intentionally smaller and easier to audit than the experimental full EKF in `goflying`. Its key changes are:

- **Quaternion gyro propagation.** Gyro rates drive the fast attitude response in quaternion space, avoiding Euler-angle singularities during propagation.
- **Adaptive accelerometer trust.** The accelerometer is treated as a gravity reference only when its magnitude is close to 1 G. During acceleration, turbulence or higher-G turns its correction weight is reduced automatically.
- **Stationary detection.** Low groundspeed, low gyro activity and a stable ~1 G acceleration vector are combined before the estimator declares the receiver stationary.
- **Runtime gyro-bias learning.** While stationary, residual gyro bias is learned slowly in RAM so drift is reduced without continuously writing the SD card.
- **Bounded attitude correction.** Accelerometer corrections are limited per sample and rejected when the innovation is too large, preventing one transient from dragging the horizon.
- **Stationary recovery.** If a clearly stationary receiver develops an attitude grossly inconsistent with gravity, v2 re-aligns roll/pitch to the gravity solution instead of remaining near an inverted solution.
- **Soft GPS yaw constraint.** GPS track is used only at useful flying speeds and only as a slow yaw-drift constraint. It does not directly drive roll or pitch, and it is not treated as identical to aircraft heading in wind.
- **Confidence score.** V2 calculates an internal 0–100% confidence value from sensor validity, accelerometer consistency, motion state and innovation size. A low-confidence solution is marked invalid rather than being published as trustworthy attitude.
- **Parallel legacy path.** Both algorithms continue running, so switching modes is immediate and the original upstream solution remains available for comparison.

## ForeFlight / GDL90 behavior

The selected algorithm controls the values written to `mySituation.AHRSRoll`, `AHRSPitch`, `AHRSGyroHeading`, `AHRSSlipSkid`, `AHRSTurnRate` and `AHRSGLoad`. Stratux's existing ForeFlight AHRS/GDL90 sender reads those fields, so ForeFlight receives the selected estimator's solution rather than a separate visual-only correction.

## Calibration and orientation

Both algorithms use the same existing Stratux orientation and calibration settings:

- `SensorQuaternion`
- accelerometer calibration vector `C`
- gyro-bias vector `D`
- **Set AHRS Sensor Orientation**
- **Set Level / Cage**
- **Zero Drift / Calibrate AHRS**

Changing algorithms does not require repeating sensor orientation unless the physical mounting changes.

## Beta status and limitations

Adaptive AHRS v2 is experimental and is **not a certified flight instrument**. Validate it against known attitude references before relying on it. The magnetometer is deliberately not used as a primary yaw reference in v2 because an uncalibrated magnetometer inside a Raspberry Pi/SDR installation is easily disturbed by current, wiring and nearby electronics. GPS track therefore helps only with slow yaw drift when moving.

The objective of v2 is not to turn inexpensive MEMS hardware into a certified attitude indicator. It is to make failure behavior more predictable: use each sensor only when it is informative, reject implausible updates, recover cleanly when stationary, and expose confidence instead of silently presenting every numerical solution as valid.
