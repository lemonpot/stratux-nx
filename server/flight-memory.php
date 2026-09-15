<?php
declare(strict_types=1);

function nx_user_flight(string $flightId, string $userId): ?array {
    if (!preg_match('/^[a-f0-9-]{36}$/i', $flightId)) return null;
    $stmt = nx_db()->prepare(
        'SELECT f.id, f.installation_id, f.aircraft_id, f.client_flight_id, f.payload, f.off_block_utc,
                f.landing_utc, f.departure_code, f.arrival_code, f.air_time_seconds,
                f.distance_nm, f.max_altitude_ft, f.created_at, i.nickname,
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

    $arrivalAirport = is_array($payload['ArrivalAirport'] ?? null) ? $payload['ArrivalAirport'] : [];
    $hasAirportElevation = array_key_exists('ElevationFt', $arrivalAirport) &&
        is_numeric($arrivalAirport['ElevationFt']) && is_finite((float)$arrivalAirport['ElevationFt']);
    $airportElevation = $hasAirportElevation ? (float)$arrivalAirport['ElevationFt'] : 0.0;
    $segment = [];
    $finalMinute = [];
    $touchdown = null;
    $touchdownDistance = PHP_INT_MAX;
    foreach ($track as $point) {
        $timestamp = strtotime($point['TimeUTC']);
        if ($timestamp === false) continue;
        $seconds = $timestamp - $landingAt;
        if ($seconds >= -300 && $seconds <= 90) {
            $point['SecondsToTouchdown'] = $seconds;
            if ($hasAirportElevation) $point['HeightAboveAirportFt'] = $point['AltitudeFt'] - $airportElevation;
            $segment[] = $point;
        }
        if ($seconds >= -60 && $seconds <= -5) $finalMinute[] = $point;
        if (abs($seconds) < $touchdownDistance) {
            $touchdownDistance = abs($seconds);
            $touchdown = $point;
        }
    }
    if (count($segment) < 2 || !$touchdown) {
        return ['available' => false, 'reason' => 'The recorded track does not cover the landing'];
    }

    $gaps = [];
    for ($index = 1; $index < count($segment); $index++) {
        $previous = strtotime($segment[$index - 1]['TimeUTC']);
        $current = strtotime($segment[$index]['TimeUTC']);
        if ($previous !== false && $current !== false && $current > $previous) $gaps[] = $current - $previous;
    }
    $maximumGap = $gaps ? max($gaps) : 0;
    $medianGap = nx_median($gaps);
    $confidence = 'low';
    if ($hasDetectedLanding && count($finalMinute) >= 8 && $maximumGap <= 15 && $touchdownDistance <= 10) {
        $confidence = 'high';
    } elseif ($hasDetectedLanding && count($finalMinute) >= 4 && $maximumGap <= 30 && $touchdownDistance <= 20) {
        $confidence = 'medium';
    }

    $speeds = array_column($finalMinute, 'GroundSpeedKt');
    $verticalFpm = array_map(static fn(array $point): float => $point['VerticalSpeedFps'] * 60, $finalMinute);
    $speedRange = $speeds ? max($speeds) - min($speeds) : 0.0;
    $verticalRange = $verticalFpm ? max($verticalFpm) - min($verticalFpm) : 0.0;
    $averageDescent = $verticalFpm ? array_sum($verticalFpm) / count($verticalFpm) : 0.0;

    $observations = [];
    $observations[] = sprintf(
        'Final-minute groundspeed ranged from %d to %d kt.',
        (int)round($speeds ? min($speeds) : $touchdown['GroundSpeedKt']),
        (int)round($speeds ? max($speeds) : $touchdown['GroundSpeedKt'])
    );
    $observations[] = sprintf(
        'Recorded vertical speed varied by approximately %d fpm in the final minute.',
        (int)round(abs($verticalRange))
    );
    if ($medianGap > 0) {
        $observations[] = sprintf('GPS samples were typically %.0f seconds apart.', $medianGap);
    }

    return [
        'available' => true,
        'confidence' => $confidence,
        'height_reference' => $hasAirportElevation ? 'airport_elevation' : 'gps_msl',
        'confidence_reasons' => [
            'detected_landing_time' => $hasDetectedLanding,
            'final_minute_samples' => count($finalMinute),
            'maximum_sample_gap_seconds' => $maximumGap,
            'touchdown_sample_offset_seconds' => $touchdownDistance,
        ],
        'touchdown' => [
            'time_utc' => gmdate('c', $landingAt),
            'groundspeed_kt' => round($touchdown['GroundSpeedKt'], 1),
            'vertical_speed_fpm' => (int)round($touchdown['VerticalSpeedFps'] * 60),
            'altitude_ft' => round($touchdown['AltitudeFt'], 1),
            'height_above_airport_ft' => $hasAirportElevation ? round($touchdown['AltitudeFt'] - $airportElevation, 1) : null,
        ],
        'final_minute' => [
            'average_vertical_speed_fpm' => (int)round($averageDescent),
            'groundspeed_range_kt' => round($speedRange, 1),
            'vertical_speed_range_fpm' => (int)round(abs($verticalRange)),
        ],
        'segment' => nx_downsample_track($segment, 180),
        'observations' => $observations,
        'limitations' => [
            'Touchdown timing and vertical speed are GPS-derived estimates, not certified flight-instrument measurements.',
            'Runway alignment and crosswind are not shown until runway and time-matched weather data are available.',
        ],
    ];
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
                'height_reference' => $landing['height_reference'],
                'touchdown' => $landing['touchdown'],
                'final_minute' => $landing['final_minute'],
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
