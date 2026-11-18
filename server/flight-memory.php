<?php
declare(strict_types=1);

function nx_user_flight(string $flightId, string $userId): ?array {
    if (!preg_match('/^[a-f0-9-]{36}$/i', $flightId)) return null;
    $stmt = nx_db()->prepare(
        'SELECT f.id, f.installation_id, f.aircraft_id, f.client_flight_id, f.payload, f.off_block_utc,
                f.landing_utc, f.departure_code, f.arrival_code, f.air_time_seconds,
                f.distance_nm, f.max_altitude_ft, f.created_at, f.share_token, i.nickname,
                a.registration aircraft_registration, a.manufacturer aircraft_manufacturer,
                a.model aircraft_model, a.nickname aircraft_nickname
         FROM flights f
         JOIN installations i ON i.id=f.installation_id
         LEFT JOIN aircraft_profiles a ON a.id=f.aircraft_id AND a.user_id=i.user_id
         WHERE f.id=? AND i.user_id=?'
    );
    $stmt->execute([$flightId, $userId]);
    return $stmt->fetch() ?: null;
}

function nx_shared_flight(string $flightId, string $shareToken): ?array {
    if (!preg_match('/^[a-f0-9-]{36}$/i', $flightId)) return null;
    if (!preg_match('/^[a-f0-9]{32}$/', $shareToken)) return null;
    $stmt = nx_db()->prepare(
        'SELECT f.id, f.installation_id, f.aircraft_id, f.client_flight_id, f.payload, f.off_block_utc,
                f.landing_utc, f.departure_code, f.arrival_code, f.air_time_seconds,
                f.distance_nm, f.max_altitude_ft, f.created_at, f.share_token,
                i.nickname,
                a.registration aircraft_registration, a.manufacturer aircraft_manufacturer,
                a.model aircraft_model, a.nickname aircraft_nickname
         FROM flights f
         JOIN installations i ON i.id=f.installation_id
         LEFT JOIN aircraft_profiles a ON a.id=f.aircraft_id AND a.user_id=i.user_id
         WHERE f.id=? AND f.share_token=?'
    );
    $stmt->execute([$flightId, $shareToken]);
    return $stmt->fetch() ?: null;
}

function nx_flight_payload(array $row): array {
    $payload = json_decode((string)($row['payload'] ?? ''), true);
    if (!is_array($payload)) throw new RuntimeException('Flight data is unavailable');
    return $payload;
}

function nx_flight_track(array $payload): array {
    $track = [];
    foreach (($payload['Track'] ?? []) as $point) {
        if (!is_array($point)) continue;
        $latitude = filter_var($point['Latitude'] ?? null, FILTER_VALIDATE_FLOAT);
        $longitude = filter_var($point['Longitude'] ?? null, FILTER_VALIDATE_FLOAT);
        if ($latitude === false || $longitude === false ||
            !is_finite((float)$latitude) || !is_finite((float)$longitude) ||
            $latitude < -90 || $latitude > 90 || $longitude < -180 || $longitude > 180) {
            continue;
        }
        $time = (string)($point['TimeUTC'] ?? '');
        if ($time === '' || strtotime($time) === false) continue;
        $track[] = [
            'TimeUTC' => $time,
            'Latitude' => (float)$latitude,
            'Longitude' => (float)$longitude,
                'AltitudeFt' => nx_bounded_metric($point['AltitudeFt'] ?? 0, -2000, 100000),
                'GroundSpeedKt' => nx_bounded_metric($point['GroundSpeedKt'] ?? 0, 0, 2000),
                'CourseDeg' => nx_bounded_metric($point['CourseDeg'] ?? 0, 0, 360),
                'VerticalSpeedFps' => nx_bounded_metric($point['VerticalSpeedFps'] ?? 0, -500, 500),
        ];
    }
    return $track;
}

function nx_finite_metric(mixed $value): float {
    if (!is_numeric($value)) return 0.0;
    $number = (float)$value;
    return is_finite($number) ? $number : 0.0;
}

function nx_bounded_metric(mixed $value, float $minimum, float $maximum): float {
    $number = nx_finite_metric($value);
    if ($number < $minimum || $number > $maximum) return 0.0;
    return $number;
}

function nx_downsample_track(array $track, int $maximum): array {
    $count = count($track);
    if ($maximum < 2 || $count <= $maximum) return $track;
    $result = [];
    $step = ($count - 1) / ($maximum - 1);
    for ($index = 0; $index < $maximum; $index++) {
        $result[] = $track[(int)round($index * $step)];
    }
    return $result;
}

function nx_median(array $values): float {
    if (!$values) return 0.0;
    sort($values, SORT_NUMERIC);
    $middle = intdiv(count($values), 2);
    if (count($values) % 2) return (float)$values[$middle];
    return ((float)$values[$middle - 1] + (float)$values[$middle]) / 2;
}

function nx_distance_ft(array $from, array $to): float {
    $lat1 = deg2rad($from['Latitude']);
    $lat2 = deg2rad($to['Latitude']);
    $h = sin(($lat2 - $lat1) / 2) ** 2 +
        cos($lat1) * cos($lat2) * sin(deg2rad($to['Longitude'] - $from['Longitude']) / 2) ** 2;
    return 2 * 20902231.0 * asin(min(1.0, sqrt($h)));
}

function nx_course_difference(float $a, float $b): float {
    return abs(fmod($a - $b + 540.0, 360.0) - 180.0);
}

function nx_landing_analysis(array $payload): array {
    $track = nx_flight_track($payload);
    usort($track, static fn(array $a, array $b): int =>
        strtotime($a['TimeUTC']) <=> strtotime($b['TimeUTC'])
    );
    $landingAt = strtotime((string)($payload['LandingUTC'] ?? ''));
    $hasDetectedLanding = $landingAt !== false;
    if ($landingAt === false && $track) $landingAt = strtotime($track[count($track) - 1]['TimeUTC']);
    if ($landingAt === false || count($track) < 2) {
        return ['available' => false, 'reason' => 'Not enough GPS data to reconstruct this landing'];
    }

    $window = [];
    foreach ($track as $point) {
        $seconds = strtotime($point['TimeUTC']) - $landingAt;
        if ($seconds >= -330 && $seconds <= 120) {
            $point['SecondsToTouchdown'] = $seconds;
            $window[] = $point;
        }
    }
    if (count($window) < 2) {
        return ['available' => false, 'reason' => 'The recorded track does not cover the landing'];
    }

    // Heights are measured from the altitude GPS reported on the rollout, so a GPS
    // offset from the published field elevation cancels out.
    $rollout = [];
    foreach ($window as $point) {
        if ($point['SecondsToTouchdown'] >= 15 && $point['SecondsToTouchdown'] <= 45 && $point['GroundSpeedKt'] > 3) {
            $rollout[] = $point['AltitudeFt'];
        }
    }
    if (!$rollout) {
        foreach ($window as $point) if ($point['SecondsToTouchdown'] >= 0) $rollout[] = $point['AltitudeFt'];
    }
    $runwayAltitude = $rollout ? nx_median($rollout) : $window[count($window) - 1]['AltitudeFt'];

    // The receiver flags a landing below 60 kt near field elevation, which can be
    // several seconds before the wheels touch. Use the first sample near the runway.
    $offset = 0;
    foreach ($window as $point) {
        if ($point['SecondsToTouchdown'] >= -30 && $point['SecondsToTouchdown'] <= 20 &&
            $point['AltitudeFt'] - $runwayAltitude <= 12) {
            $offset = $point['SecondsToTouchdown'];
            break;
        }
    }
    $touchdownAt = $landingAt + $offset;

    $segment = [];
    $touchdownIndex = null;
    $touchdownDistance = PHP_INT_MAX;
    $finalMinuteSamples = 0;
    foreach ($window as $point) {
        $seconds = $point['SecondsToTouchdown'] - $offset;
        if ($seconds < -300 || $seconds > 90) continue;
        $point['SecondsToTouchdown'] = $seconds;
        $point['HeightAboveRunwayFt'] = round($point['AltitudeFt'] - $runwayAltitude, 1);
        $segment[] = $point;
        if ($seconds >= -60 && $seconds <= -5) $finalMinuteSamples++;
        if (abs($seconds) < $touchdownDistance) {
            $touchdownDistance = abs($seconds);
            $touchdownIndex = count($segment) - 1;
        }
    }
    if (count($segment) < 2 || $touchdownIndex === null) {
        return ['available' => false, 'reason' => 'The recorded track does not cover the landing'];
    }
    $touchdown = $segment[$touchdownIndex];
    if (abs($touchdown['HeightAboveRunwayFt']) > 30) {
        return ['available' => false, 'reason' => 'The touchdown could not be identified in the GPS track'];
    }
    $approachHeight = max(array_column(array_slice($segment, 0, $touchdownIndex + 1), 'HeightAboveRunwayFt'));
    if ($approachHeight < 100) {
        return ['available' => false, 'reason' => 'No approach was recorded before this landing'];
    }

    $maximumGap = 0;
    for ($index = 1; $index < count($segment); $index++) {
        $maximumGap = max($maximumGap, $segment[$index]['SecondsToTouchdown'] - $segment[$index - 1]['SecondsToTouchdown']);
    }
    $confidence = 'low';
    if ($hasDetectedLanding && $finalMinuteSamples >= 8 && $maximumGap <= 15 && $touchdownDistance <= 10) {
        $confidence = 'high';
    } elseif ($hasDetectedLanding && $finalMinuteSamples >= 4 && $maximumGap <= 30 && $touchdownDistance <= 20) {
        $confidence = 'medium';
    }

    // Final approach: the straight segment ending at touchdown, on the touchdown course.
    $x = 0.0;
    $y = 0.0;
    foreach ($segment as $point) {
        if ($point['SecondsToTouchdown'] >= -10 && $point['SecondsToTouchdown'] <= 0 && $point['GroundSpeedKt'] > 30) {
            $x += cos(deg2rad($point['CourseDeg']));
            $y += sin(deg2rad($point['CourseDeg']));
        }
    }
    $linedUpIndex = null;
    if ($x != 0.0 || $y != 0.0) {
        $finalCourse = fmod(rad2deg(atan2($y, $x)) + 360.0, 360.0);
        for ($index = $touchdownIndex; $index >= 0; $index--) {
            $point = $segment[$index];
            if ($point['GroundSpeedKt'] < 30 || nx_course_difference($point['CourseDeg'], $finalCourse) > 12) break;
            if ($index < $touchdownIndex &&
                $segment[$index + 1]['SecondsToTouchdown'] - $point['SecondsToTouchdown'] > 15) break;
            $linedUpIndex = $index;
        }
    }
    $linedUp = $linedUpIndex !== null && $linedUpIndex < $touchdownIndex ? $segment[$linedUpIndex] : null;
    $linedUpSeconds = $linedUp ? $linedUp['SecondsToTouchdown'] : null;
    $linedUpHeight = $linedUp ? $linedUp['HeightAboveRunwayFt'] : null;
    $finalDistance = 0.0;
    if ($linedUp) {
        for ($index = $linedUpIndex; $index < $touchdownIndex; $index++) {
            $finalDistance += nx_distance_ft($segment[$index], $segment[$index + 1]);
        }
    }
    $pathAngle = null;
    $averageDescent = null;
    if ($linedUp && $linedUpHeight - $touchdown['HeightAboveRunwayFt'] >= 100 && $finalDistance >= 500) {
        $drop = $linedUpHeight - $touchdown['HeightAboveRunwayFt'];
        $pathAngle = rad2deg(atan2($drop, $finalDistance));
        $averageDescent = $drop / ($touchdown['SecondsToTouchdown'] - $linedUpSeconds) * 60;
    }

    // Slowing down on final is normal; speed gained on short final is what matters.
    // Turns change groundspeed with the wind, so only the straight final is used.
    $speedStart = $linedUp ? max($linedUpSeconds, -45) : -30;
    $speeds = [];
    $speedGain = 0.0;
    $gainSeconds = null;
    $gainFrom = null;
    foreach ($segment as $point) {
        if ($point['SecondsToTouchdown'] < $speedStart || $point['SecondsToTouchdown'] > -3) continue;
        if ($speeds && $point['GroundSpeedKt'] - min($speeds) > $speedGain) {
            $speedGain = $point['GroundSpeedKt'] - min($speeds);
            $gainSeconds = $point['SecondsToTouchdown'];
            $gainFrom = min($speeds);
        }
        $speeds[] = $point['GroundSpeedKt'];
    }

    $rolloutSeconds = null;
    $rolloutDistance = 0.0;
    for ($index = $touchdownIndex; $index < count($segment) - 1; $index++) {
        if ($segment[$index]['GroundSpeedKt'] <= 15) break;
        $rolloutDistance += nx_distance_ft($segment[$index], $segment[$index + 1]);
    }
    if ($index > $touchdownIndex && $segment[$index]['GroundSpeedKt'] <= 15) {
        $rolloutSeconds = $segment[$index]['SecondsToTouchdown'];
    }

    $checks = [];
    if ($linedUp) {
        $good = $linedUpHeight >= 500;
        $where = sprintf('%s ft, %s (%.1f NM) from the runway', number_format(max(0, $linedUpHeight)), nx_duration_words(-$linedUpSeconds), $finalDistance / 6076.12);
        $checks[] = [
            'label' => 'Final approach',
            'verdict' => $good ? 'good' : 'check',
            'value' => sprintf('Lined up at %s ft', number_format(max(0, $linedUpHeight))),
            'detail' => $good
                ? "You rolled out on final at $where. That gave you time to settle speed and descent before the flare."
                : "You rolled out on final at only $where. That is a short final: speed, descent and alignment all have to be fixed in very little time, so any correction happens close to the ground.",
            'tip' => $good ? null : 'Extend downwind a little or start the base turn later, so you are lined up by 500 ft, about 1.5 NM out on a normal approach.',
        ];
    }
    if ($pathAngle !== null) {
        $path = sprintf('%.1f° (about %s fpm)', $pathAngle, number_format($averageDescent, 0));
        if ($pathAngle < 2.5) {
            $verdict = 'check';
            $detail = "You came down at only $path, flatter than the normal 3°. A low, dragged-in approach keeps you near obstacles with power on and leaves few options if the engine quits.";
            $tip = 'Reduce power a little earlier and keep your aim point steady in the windshield: if it moves up, you are getting low. A PAPI should show two white and two red.';
        } elseif ($pathAngle > 5.0) {
            $verdict = 'check';
            $detail = "You came down at $path, steeper than the normal 3°. A steep path means a high sink rate near the ground, which makes the flare harder to time and can end in a firm landing.";
            $tip = 'Start the descent a little earlier and keep your aim point fixed in the windshield. If the runway has a PAPI, aim for two white and two red.';
        } else {
            $verdict = 'good';
            $detail = $pathAngle <= 3.7
                ? "You came down at $path, close to the normal 3° of a visual approach. A steady path like this makes the flare easier to judge."
                : "You came down at $path, a bit steeper than the usual 3° but still normal for a light aircraft.";
            $tip = null;
        }
        $checks[] = ['label' => 'Approach path', 'verdict' => $verdict, 'value' => sprintf('%.1f° slope', $pathAngle), 'detail' => $detail, 'tip' => $tip];
    }
    if (count($speeds) >= 2) {
        $good = $speedGain < 8;
        $checks[] = [
            'label' => 'Speed on short final',
            'verdict' => $good ? 'good' : 'check',
            'value' => sprintf('%d–%d kt', (int)round(min($speeds)), (int)round(max($speeds))),
            'detail' => $good
                ? sprintf('Your groundspeed held steady or came down on short final (%d–%d kt). That is what a stable approach looks like.', (int)round(min($speeds)), (int)round(max($speeds)))
                : sprintf('Your groundspeed rose from %d to %d kt on short final, peaking %s before touchdown. If the wind did not change, the nose probably dropped or power came in. Extra speed this close to the runway usually means floating in the flare and a longer landing.',
                    (int)round($gainFrom), (int)round($gainFrom + $speedGain), nx_duration_words(-$gainSeconds)),
            'tip' => $good ? null : 'Set your approach speed and trim for it as soon as you are on final, then make small, early corrections. In gusty wind, plan any extra speed before final, not on it.',
        ];
    }
    $checks[] = [
        'label' => 'Touchdown',
        'verdict' => 'info',
        'value' => sprintf('%d kt groundspeed', (int)round($touchdown['GroundSpeedKt'])),
        'detail' => sprintf('You touched down at %d kt groundspeed.', (int)round($touchdown['GroundSpeedKt'])),
        'tip' => null,
    ];
    $checks[] = [
        'label' => 'Rollout',
        'verdict' => 'info',
        'value' => $rolloutSeconds === null ? 'Not recorded' : sprintf('%d s · %s ft', $rolloutSeconds, number_format(round($rolloutDistance, -1))),
        'detail' => $rolloutSeconds === null
            ? 'The recording ends before the aircraft slowed to taxi speed.'
            : sprintf('It took %d seconds and about %s ft to slow to taxi speed (15 kt).', $rolloutSeconds, number_format(round($rolloutDistance, -1))),
        'tip' => null,
    ];

    $story = '';
    if ($linedUp) {
        $story = sprintf('You rolled out on final at %s ft, %s from the runway', number_format(max(0, $linedUpHeight)), nx_duration_words(-$linedUpSeconds)) .
            ($pathAngle === null ? '. ' : sprintf(', and came down on a %.1f° path. ', $pathAngle));
    }
    if (count($speeds) >= 2) {
        $story .= $speedGain >= 8
            ? sprintf('Your groundspeed rose from %d to %d kt on short final, then you', (int)round($gainFrom), (int)round($gainFrom + $speedGain))
            : 'Your speed held steady on short final, then you';
    } else {
        $story .= 'You';
    }
    $story .= sprintf(' touched down at %d kt', (int)round($touchdown['GroundSpeedKt'])) .
        ($rolloutSeconds === null ? '.' : sprintf(' and slowed to taxi speed in %d seconds.', $rolloutSeconds));

    return [
        'available' => true,
        'confidence' => $confidence,
        'confidence_reasons' => [
            'detected_landing_time' => $hasDetectedLanding,
            'final_minute_samples' => $finalMinuteSamples,
            'maximum_sample_gap_seconds' => $maximumGap,
            'touchdown_sample_offset_seconds' => $touchdownDistance,
        ],
        'touchdown' => [
            'time_utc' => gmdate('c', $touchdownAt),
            'groundspeed_kt' => round($touchdown['GroundSpeedKt'], 1),
        ],
        'approach' => [
            'lined_up_seconds' => $linedUpSeconds,
            'lined_up_height_ft' => $linedUpHeight === null ? null : (int)round(max(0, $linedUpHeight)),
            'path_angle_deg' => $pathAngle === null ? null : round($pathAngle, 1),
            'speed_min_kt' => $speeds ? (int)round(min($speeds)) : null,
            'speed_max_kt' => $speeds ? (int)round(max($speeds)) : null,
            'speed_gain_kt' => (int)round($speedGain),
        ],
        'rollout' => [
            'seconds' => $rolloutSeconds,
            'distance_ft' => $rolloutSeconds === null ? null : (int)round($rolloutDistance, -1),
        ],
        'summary' => nx_landing_summary($checks, $story, $confidence),
        'checks' => $checks,
        'segment' => nx_downsample_track($segment, 180),
    ];
}

function nx_duration_words(float $seconds): string {
    $seconds = (int)round(max(0, $seconds));
    if ($seconds < 60) return $seconds . ' seconds';
    return intdiv($seconds, 60) . ' min ' . ($seconds % 60) . ' s';
}

function nx_landing_summary(array $checks, string $story, string $confidence): array {
    $tips = array_values(array_filter(array_column($checks, 'tip')));
    return [
        'title' => $tips ? count($tips) . (count($tips) === 1 ? ' thing' : ' things') . ' to improve' : 'Stable approach',
        'story' => $story,
        'tips' => $tips,
        'note' => $confidence === 'low' ? 'GPS data was limited on this landing, so treat these as rough estimates.' : '',
    ];
}

// Adds how this touchdown and rollout compare with previous landings at the same airport.
function nx_landing_with_history(array $landing, array $previous): array {
    if (!($landing['available'] ?? false) || !$previous) return $landing;
    $count = count($previous);
    $here = $count === 1 ? 'your previous landing here' : "your last $count landings here";
    $verdicts = array_column($landing['checks'], 'verdict', 'label');
    $gainedSpeed = ($verdicts['Speed on short final'] ?? '') === 'check';
    $story = $landing['summary']['story'];

    $usualSpeed = array_sum(array_map(static fn(array $p): float => (float)$p['touchdown']['groundspeed_kt'], $previous)) / $count;
    $speedDifference = (int)round($landing['touchdown']['groundspeed_kt'] - $usualSpeed);
    $fastTouchdown = $speedDifference >= 6;
    $rollouts = array_values(array_filter(array_map(static fn(array $p): ?int => $p['rollout']['seconds'] ?? null, $previous), static fn(?int $s): bool => $s !== null));
    $usualRollout = $rollouts ? array_sum($rollouts) / count($rollouts) : null;
    $rolloutDifference = $usualRollout === null || $landing['rollout']['seconds'] === null ? null : (int)round($landing['rollout']['seconds'] - $usualRollout);

    foreach ($landing['checks'] as &$check) {
        if ($check['label'] === 'Touchdown') {
            if (abs($speedDifference) < 4) {
                $check['detail'] .= sprintf(' That is about the same as %s (%d kt).', $here, (int)round($usualSpeed));
            } else {
                $check['detail'] .= sprintf(' That is %d kt %s than %s (%d kt). Wind changes groundspeed, so compare days with similar wind.',
                    abs($speedDifference), $speedDifference > 0 ? 'faster' : 'slower', $here, (int)round($usualSpeed));
            }
            if ($fastTouchdown) {
                $check['verdict'] = 'check';
                if ($gainedSpeed) {
                    $check['detail'] .= ' The speed you gained on short final probably carried into the touchdown.';
                } else {
                    $check['tip'] = 'If the wind was similar, you arrived fast or flared early. Cross the threshold at your usual approach speed and let the speed bleed off in the flare.';
                }
            }
        }
        if ($check['label'] === 'Rollout' && $rolloutDifference !== null) {
            $check['detail'] .= abs($rolloutDifference) < 10
                ? sprintf(' About the same as usual here (%d s).', (int)round($usualRollout))
                : sprintf(' That is %d seconds %s than usual here (%d s).', abs($rolloutDifference), $rolloutDifference > 0 ? 'longer' : 'shorter', (int)round($usualRollout));
            if ($rolloutDifference >= 20) {
                $check['verdict'] = 'check';
                if ($fastTouchdown) {
                    $check['detail'] .= ' A faster touchdown takes longer to stop, so this follows from the extra speed.';
                } else {
                    $check['tip'] = 'A long rollout usually follows a faster touchdown or later braking. If you did not mean to let it roll, start braking once the nosewheel is down.';
                }
            }
        }
    }
    unset($check);

    if ($fastTouchdown) {
        $story .= sprintf(' That touchdown was %d kt faster than usual here', $speedDifference) .
            ($rolloutDifference !== null && $rolloutDifference >= 20 ? sprintf(' and the rollout %d seconds longer.', $rolloutDifference) : '.');
    } elseif (abs($speedDifference) < 4 && $rolloutDifference !== null && abs($rolloutDifference) < 10) {
        $story .= ' Touchdown speed and rollout were in line with your usual landings here.';
    }
    $landing['summary'] = nx_landing_summary($landing['checks'], $story, $landing['confidence']);
    return $landing;
}

function nx_comparable_landings(array $currentFlight, string $userId, int $maximum = 3): array {
    $arrival = strtoupper(trim((string)($currentFlight['arrival_code'] ?? '')));
    if ($arrival === '') return [];
    $aircraftId = trim((string)($currentFlight['aircraft_id'] ?? ''));
    $stmt = nx_db()->prepare(
        'SELECT f.id, f.payload, f.off_block_utc, f.departure_code, f.arrival_code
         FROM flights f
         JOIN installations i ON i.id=f.installation_id
         WHERE i.user_id=? AND f.installation_id=? AND f.id<>? AND f.arrival_code=?
           AND (?="" OR f.aircraft_id=?)
           AND COALESCE(NULLIF(f.off_block_utc,""),f.created_at) < COALESCE(NULLIF(?,""),?)
         ORDER BY COALESCE(NULLIF(f.off_block_utc,""),f.created_at) DESC
         LIMIT ?'
    );
    $stmt->bindValue(1, $userId);
    $stmt->bindValue(2, (string)$currentFlight['installation_id']);
    $stmt->bindValue(3, (string)$currentFlight['id']);
    $stmt->bindValue(4, $arrival);
    $stmt->bindValue(5, $aircraftId);
    $stmt->bindValue(6, $aircraftId);
    $stmt->bindValue(7, (string)$currentFlight['off_block_utc']);
    $stmt->bindValue(8, (string)$currentFlight['created_at']);
    $stmt->bindValue(9, max(1, min(10, $maximum)), PDO::PARAM_INT);
    $stmt->execute();

    $comparisons = [];
    foreach ($stmt->fetchAll() as $row) {
        try {
            $payload = nx_flight_payload($row);
            $landing = nx_landing_analysis($payload);
            if (!($landing['available'] ?? false)) continue;
            $comparisons[] = [
                'flight_id' => $row['id'],
                'date_utc' => $row['off_block_utc'],
                'route' => ($row['departure_code'] ?: '---') . ' → ' . ($row['arrival_code'] ?: '---'),
                'confidence' => $landing['confidence'],
                'touchdown' => $landing['touchdown'],
                'approach' => $landing['approach'],
                'rollout' => $landing['rollout'],
                'segment' => $landing['segment'],
            ];
        } catch (Throwable $e) {
            continue;
        }
    }
    return $comparisons;
}

function nx_pilot_summary(string $userId): array {
    $stmt = nx_db()->prepare(
        'SELECT COUNT(*) flights, COALESCE(SUM(f.air_time_seconds),0) air_time_seconds,
                COALESCE(SUM(f.distance_nm),0) distance_nm, COALESCE(MAX(f.distance_nm),0) longest_nm
         FROM flights f JOIN installations i ON i.id=f.installation_id WHERE i.user_id=?'
    );
    $stmt->execute([$userId]);
    $summary = $stmt->fetch() ?: [];
    $stmt = nx_db()->prepare(
        'SELECT f.departure_code,f.arrival_code
         FROM flights f JOIN installations i ON i.id=f.installation_id WHERE i.user_id=?'
    );
    $stmt->execute([$userId]);
    $airports = [];
    foreach ($stmt->fetchAll() as $row) {
        foreach ([$row['departure_code'], $row['arrival_code']] as $code) {
            $code = strtoupper(trim((string)$code));
            if ($code !== '') $airports[$code] = ($airports[$code] ?? 0) + 1;
        }
    }
    arsort($airports);
    $topAirport = $airports ? (string)array_key_first($airports) : '';
    $stmt = nx_db()->prepare('SELECT COUNT(*) FROM aircraft_profiles WHERE user_id=?');
    $stmt->execute([$userId]);
    $aircraftCount = (int)$stmt->fetchColumn();
    $stmt = nx_db()->prepare('SELECT COUNT(*) FROM flights f JOIN installations i ON i.id=f.installation_id WHERE i.user_id=? AND COALESCE(NULLIF(f.off_block_utc,""),f.created_at)>=?');
    $stmt->execute([$userId, gmdate(DATE_RFC3339, time() - 30 * 86400)]);
    $recentFlights = (int)$stmt->fetchColumn();
    $stmt = nx_db()->prepare(
        'SELECT a.nickname,a.registration,a.model,COUNT(*) flights
         FROM flights f JOIN installations i ON i.id=f.installation_id
         JOIN aircraft_profiles a ON a.id=f.aircraft_id AND a.user_id=i.user_id
         WHERE i.user_id=? GROUP BY a.id ORDER BY flights DESC,a.created_at ASC LIMIT 1'
    );
    $stmt->execute([$userId]);
    $topAircraft = $stmt->fetch();
    $topAircraftLabel = $topAircraft ? aircraft_label_for_memory([
        'aircraft_nickname' => $topAircraft['nickname'],
        'aircraft_registration' => $topAircraft['registration'],
        'aircraft_model' => $topAircraft['model'],
    ]) : '';
    $flights = (int)($summary['flights'] ?? 0);
    return [
        'flights' => $flights,
        'air_time_seconds' => (int)($summary['air_time_seconds'] ?? 0),
        'distance_nm' => round((float)($summary['distance_nm'] ?? 0), 1),
        'aircraft' => $aircraftCount,
        'airports' => count($airports),
        'average_air_time_seconds' => $flights ? (int)round((int)$summary['air_time_seconds'] / $flights) : 0,
        'longest_nm' => round((float)($summary['longest_nm'] ?? 0), 1),
        'recent_flights' => $recentFlights,
        'top_airport' => $topAirport,
        'top_aircraft' => $topAircraftLabel,
    ];
}

function nx_flight_milestones(array $flight, string $userId): array {
    $sortTime = (string)($flight['off_block_utc'] ?: $flight['created_at']);
    $db = nx_db();
    $priorBase =
        ' FROM flights f JOIN installations i ON i.id=f.installation_id
          WHERE i.user_id=? AND f.id<>?
            AND COALESCE(NULLIF(f.off_block_utc,""),f.created_at) < ?';
    $stmt = $db->prepare('SELECT COUNT(*) flights, COALESCE(SUM(f.air_time_seconds),0) seconds, COALESCE(MAX(f.distance_nm),0) longest' . $priorBase);
    $stmt->execute([$userId, $flight['id'], $sortTime]);
    $prior = $stmt->fetch() ?: ['flights' => 0, 'seconds' => 0, 'longest' => 0];
    $currentNumber = (int)$prior['flights'] + 1;
    $currentHoursSeconds = (int)$prior['seconds'] + (int)$flight['air_time_seconds'];
    $milestones = [];

    if ((int)$prior['flights'] === 0) {
        $milestones[] = ['kind' => 'first-flight', 'title' => 'First flight remembered', 'detail' => 'The beginning of your private Stratux NX history.'];
    }
    foreach ([10, 25, 50, 100, 250, 500, 1000] as $flightThreshold) {
        if ($currentNumber === $flightThreshold) {
            $milestones[] = ['kind' => 'flights', 'title' => $flightThreshold . ' flights recorded', 'detail' => 'A personal consistency milestone.'];
        }
    }
    foreach ([1, 10, 25, 50, 100, 250, 500, 1000] as $hourThreshold) {
        $seconds = $hourThreshold * 3600;
        if ((int)$prior['seconds'] < $seconds && $currentHoursSeconds >= $seconds) {
            $milestones[] = ['kind' => 'hours', 'title' => $hourThreshold . ' flight hours remembered', 'detail' => 'Calculated from recorded airborne time.'];
        }
    }

    $arrival = strtoupper(trim((string)$flight['arrival_code']));
    if ($arrival !== '') {
        $stmt = $db->prepare('SELECT COUNT(*)' . $priorBase . ' AND (f.departure_code=? OR f.arrival_code=?)');
        $stmt->execute([$userId, $flight['id'], $sortTime, $arrival, $arrival]);
        if ((int)$stmt->fetchColumn() === 0) {
            $milestones[] = ['kind' => 'airport', 'title' => 'New airport: ' . $arrival, 'detail' => 'Your first recorded visit to this airport.'];
        }
    }
    if ((float)$flight['distance_nm'] > 0 && (float)$flight['distance_nm'] > (float)$prior['longest']) {
        $milestones[] = [
            'kind' => 'distance',
            'title' => (int)$prior['flights'] === 0 ? 'First route recorded' : 'Longest recorded route',
            'detail' => number_format((float)$flight['distance_nm'], 1) . ' NM in this flight.',
        ];
    }
    $aircraftId = trim((string)($flight['aircraft_id'] ?? ''));
    if ($aircraftId !== '') {
        $stmt = $db->prepare('SELECT COUNT(*)' . $priorBase . ' AND f.aircraft_id=?');
        $stmt->execute([$userId, $flight['id'], $sortTime, $aircraftId]);
        if ((int)$stmt->fetchColumn() === 0) {
            $milestones[] = ['kind' => 'aircraft', 'title' => 'New aircraft in your history', 'detail' => aircraft_label_for_memory($flight)];
        }
    }

    return [
        'flight_number' => $currentNumber,
        'cumulative_air_time_seconds' => $currentHoursSeconds,
        'milestones' => $milestones,
    ];
}

function aircraft_label_for_memory(array $flight): string {
    $nickname = trim((string)($flight['aircraft_nickname'] ?? ''));
    if ($nickname !== '') return $nickname;
    $label = trim((string)($flight['aircraft_registration'] ?? '') . ' ' . (string)($flight['aircraft_model'] ?? ''));
    return $label !== '' ? $label : 'Aircraft profile';
}

function nx_flight_filename(array $payload, string $extension): string {
    $departure = preg_replace('/[^A-Z0-9-]/', '', strtoupper((string)($payload['DepartureAirport']['Code'] ?? 'DEP')));
    $arrival = preg_replace('/[^A-Z0-9-]/', '', strtoupper((string)($payload['ArrivalAirport']['Code'] ?? 'ARR')));
    $date = gmdate('Y-m-d');
    $offBlock = strtotime((string)($payload['OffBlockUTC'] ?? ''));
    if ($offBlock !== false) {
        $date = gmdate('Y-m-d', $offBlock);
    } elseif (preg_match('/^(\d{4})(\d{2})(\d{2})/', (string)($payload['ID'] ?? ''), $parts)) {
        $date = $parts[1] . '-' . $parts[2] . '-' . $parts[3];
    }
    return sprintf('stratux-nx-%s-%s-%s.%s', $date, $departure ?: 'DEP', $arrival ?: 'ARR', $extension);
}

function nx_xml(string $value): string {
    return htmlspecialchars($value, ENT_QUOTES | ENT_XML1, 'UTF-8');
}

function nx_send_flight_export(array $payload, string $format): never {
    $track = nx_flight_track($payload);
    if (count($track) < 2) {
        http_response_code(422);
        exit('This flight does not contain a usable track');
    }

    header('Cache-Control: no-store');
    header('X-Content-Type-Options: nosniff');

    if ($format === 'csv') {
        header('Content-Type: text/csv; charset=utf-8');
        header('Content-Disposition: attachment; filename="' . nx_flight_filename($payload, 'csv') . '"');
        $output = fopen('php://output', 'wb');
        fputcsv($output, ['time_utc', 'latitude', 'longitude', 'altitude_ft', 'ground_speed_kt', 'course_deg', 'vertical_speed_fps'], ',', '"', '');
        foreach ($track as $point) {
            fputcsv($output, [
                gmdate('c', (int)strtotime($point['TimeUTC'])), $point['Latitude'], $point['Longitude'],
                $point['AltitudeFt'], $point['GroundSpeedKt'],
                $point['CourseDeg'], $point['VerticalSpeedFps'],
            ], ',', '"', '');
        }
        fclose($output);
        exit;
    }

    $route = (string)($payload['DepartureAirport']['Code'] ?? 'Departure') .
        ' to ' . (string)($payload['ArrivalAirport']['Code'] ?? 'Arrival');

    if ($format === 'gpx') {
        header('Content-Type: application/gpx+xml; charset=utf-8');
        header('Content-Disposition: attachment; filename="' . nx_flight_filename($payload, 'gpx') . '"');
        echo '<?xml version="1.0" encoding="UTF-8"?>' . "\n";
        echo '<gpx version="1.1" creator="Stratux NX" xmlns="http://www.topografix.com/GPX/1/1"><trk><name>' .
            nx_xml($route) . '</name><trkseg>';
        foreach ($track as $point) {
            echo '<trkpt lat="' . $point['Latitude'] . '" lon="' . $point['Longitude'] . '"><ele>' .
                round($point['AltitudeFt'] * 0.3048, 2) . '</ele><time>' .
                nx_xml(gmdate('c', (int)strtotime($point['TimeUTC']))) . '</time></trkpt>';
        }
        echo '</trkseg></trk></gpx>';
        exit;
    }

    if ($format === 'kml') {
        header('Content-Type: application/vnd.google-earth.kml+xml; charset=utf-8');
        header('Content-Disposition: attachment; filename="' . nx_flight_filename($payload, 'kml') . '"');
        echo '<?xml version="1.0" encoding="UTF-8"?>' . "\n";
        echo '<kml xmlns="http://www.opengis.net/kml/2.2"><Document><name>' . nx_xml($route) .
            '</name><Placemark><name>Flight path</name><LineString><altitudeMode>absolute</altitudeMode><coordinates>';
        foreach ($track as $point) {
            echo $point['Longitude'] . ',' . $point['Latitude'] . ',' .
                round($point['AltitudeFt'] * 0.3048, 2) . "\n";
        }
        echo '</coordinates></LineString></Placemark></Document></kml>';
        exit;
    }

    http_response_code(400);
    exit('Unsupported export format');
}
