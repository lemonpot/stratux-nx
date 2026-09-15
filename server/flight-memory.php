<?php
declare(strict_types=1);

function nx_user_flight(string $flightId, string $userId): ?array {
    if (!preg_match('/^[a-f0-9-]{36}$/i', $flightId)) return null;
    $stmt = nx_db()->prepare(
        'SELECT f.id, f.installation_id, f.client_flight_id, f.payload, f.off_block_utc,
                f.departure_code, f.arrival_code, f.created_at, i.nickname
         FROM flights f
         JOIN installations i ON i.id=f.installation_id
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
            'AltitudeFt' => nx_finite_metric($point['AltitudeFt'] ?? 0),
            'GroundSpeedKt' => nx_finite_metric($point['GroundSpeedKt'] ?? 0),
            'CourseDeg' => nx_finite_metric($point['CourseDeg'] ?? 0),
            'VerticalSpeedFps' => nx_finite_metric($point['VerticalSpeedFps'] ?? 0),
        ];
    }
    return $track;
}

function nx_finite_metric(mixed $value): float {
    if (!is_numeric($value)) return 0.0;
    $number = (float)$value;
    return is_finite($number) ? $number : 0.0;
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
