<?php
declare(strict_types=1);

$testDir = sys_get_temp_dir() . '/stratux-nx-flight-memory-' . bin2hex(random_bytes(4));
mkdir($testDir, 0700, true);
putenv('NX_DATA_DIR=' . $testDir);

require dirname(__DIR__) . '/lib.php';
require dirname(__DIR__) . '/flight-memory.php';

function expect(bool $condition, string $message): void {
    if (!$condition) throw new RuntimeException($message);
}

$payload = [
    'ID' => '20260914T120000Z',
    'OffBlockUTC' => '2026-09-14T12:00:00Z',
    'DepartureAirport' => ['Code' => 'CYUL'],
    'ArrivalAirport' => ['Code' => 'CYQB'],
    'Track' => [
        ['TimeUTC' => '2026-09-14T12:00:00Z', 'Latitude' => 45.5, 'Longitude' => -73.5, 'AltitudeFt' => '1e400'],
        ['TimeUTC' => 'invalid', 'Latitude' => 45.55, 'Longitude' => -73.45, 'AltitudeFt' => 1500],
        ['TimeUTC' => '2026-09-14T12:01:00Z', 'Latitude' => 45.6, 'Longitude' => -73.4, 'AltitudeFt' => 2000],
        ['TimeUTC' => '2026-09-14T12:02:00Z', 'Latitude' => 45.7, 'Longitude' => -73.3, 'AltitudeFt' => 1.0e308, 'GroundSpeedKt' => 1.0e308, 'VerticalSpeedFps' => -1.0e308],
    ],
];

$validTrack = nx_flight_track($payload);
expect(count($validTrack) === 3, 'invalid track points must be removed');
expect($validTrack[0]['AltitudeFt'] === 0.0, 'non-finite metrics must be normalized');
expect($validTrack[2]['AltitudeFt'] === 0.0 && $validTrack[2]['GroundSpeedKt'] === 0.0, 'extreme finite metrics must be rejected');
expect(
    nx_downsample_track(range(0, 9), 3) === [0, 5, 9],
    'downsampling must preserve both endpoints'
);
expect(
    nx_flight_filename($payload, 'gpx') === 'stratux-nx-2026-09-14-CYUL-CYQB.gpx',
    'export filename must be stable and safe'
);

$landingAt = strtotime('2026-09-13T16:30:00Z');
$landingTrack = [];
for ($seconds = -120; $seconds <= 60; $seconds += 5) {
    $landingTrack[] = [
        'TimeUTC' => gmdate('c', $landingAt + $seconds),
        'Latitude' => 46.78 + $seconds * 0.0003,
        'Longitude' => -71.39,
        'AltitudeFt' => 250 + max(0, -$seconds) * 8,
        'GroundSpeedKt' => $seconds <= 0 ? 65 - $seconds / 20 : max(8, 65 - $seconds),
        'VerticalSpeedFps' => 0.0,
    ];
}
$landingPayload = [
    'ID' => '20260913T153000Z',
    'OffBlockUTC' => '2026-09-13T15:30:00Z',
    'LandingUTC' => gmdate('c', $landingAt),
    'ArrivalAirport' => ['Code' => 'CYQB', 'ElevationFt' => 250],
    'Track' => $landingTrack,
];
$landing = nx_landing_analysis($landingPayload);
expect($landing['available'] === true, 'landing analysis must be available with a valid segment');
expect($landing['confidence'] === 'high', 'dense GPS landing data must report high confidence');
expect(count($landing['segment']) === 37, 'landing segment must include approach and rollout samples');
expect($landing['approach']['lined_up_height_ft'] === 960, 'a straight final must be found from the start of the approach');
expect($landing['approach']['path_angle_deg'] >= 3.5 && $landing['approach']['path_angle_deg'] <= 5.0, 'approach slope must follow height over distance');
expect($landing['approach']['speed_gain_kt'] === 0, 'slowing down on final must not count as speed gained');
expect($landing['rollout']['seconds'] === 50, 'rollout must end at taxi speed');
expect(array_column($landing['checks'], 'verdict', 'label')['Speed on short final'] === 'good', 'a decelerating final is stable');

$earlyFlag = $landingPayload;
$earlyFlag['LandingUTC'] = gmdate('c', $landingAt - 5);
$earlyFlag['ArrivalAirport']['ElevationFt'] = 230;
$refined = nx_landing_analysis($earlyFlag);
expect($refined['touchdown']['time_utc'] === gmdate('c', $landingAt), 'touchdown must be refined to the first sample on the runway');
expect($refined['segment'][24]['SecondsToTouchdown'] === 0 && $refined['segment'][24]['HeightAboveRunwayFt'] === 0.0,
    'heights must be measured from the recorded runway altitude, not the published elevation');

$db = nx_db();
$db->prepare('INSERT INTO users(id,email,name,created_at) VALUES(?,?,?,?)')
    ->execute(['user-a', 'pilot@example.com', 'Pilot', gmdate(DATE_RFC3339)]);
$db->prepare('INSERT INTO users(id,email,name,created_at) VALUES(?,?,?,?)')
    ->execute(['user-b', 'other@example.com', 'Other', gmdate(DATE_RFC3339)]);
$db->prepare('INSERT INTO aircraft_profiles(id,user_id,registration,manufacturer,model,nickname,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)')
    ->execute(['aircraft-a', 'user-a', 'C-GABC', 'Cessna', '172M', 'Lemon One', gmdate(DATE_RFC3339), gmdate(DATE_RFC3339)]);
$db->prepare('INSERT INTO installations(id,public_key,user_id,nickname,created_at,last_seen_at) VALUES(?,?,?,?,?,?)')
    ->execute(['NX-11111111-1111-1111-1111-111111111111', 'key', 'user-a', 'C172', gmdate(DATE_RFC3339), gmdate(DATE_RFC3339)]);
$db->prepare('INSERT INTO flights(id,installation_id,client_flight_id,payload,content_hash,off_block_utc,departure_code,arrival_code,created_at) VALUES(?,?,?,?,?,?,?,?,?)')
    ->execute([
        'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
        'NX-11111111-1111-1111-1111-111111111111',
        $payload['ID'],
        json_encode($payload, JSON_THROW_ON_ERROR),
        hash('sha256', json_encode($payload, JSON_THROW_ON_ERROR)),
        $payload['OffBlockUTC'],
        'CYUL',
        'CYQB',
        gmdate(DATE_RFC3339),
    ]);
$landingBody = json_encode($landingPayload, JSON_THROW_ON_ERROR);
$db->prepare('INSERT INTO flights(id,installation_id,client_flight_id,payload,content_hash,off_block_utc,departure_code,arrival_code,created_at) VALUES(?,?,?,?,?,?,?,?,?)')
    ->execute([
        'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb',
        'NX-11111111-1111-1111-1111-111111111111',
        $landingPayload['ID'],
        $landingBody,
        hash('sha256', $landingBody),
        $landingPayload['OffBlockUTC'],
        'CYUL',
        'CYQB',
        '2026-09-13T17:00:00Z',
    ]);
$db->prepare('UPDATE flights SET landing_utc=?,air_time_seconds=?,distance_nm=?,max_altitude_ft=? WHERE id=?')
    ->execute(['2026-09-14T13:00:00Z', 3000, 132.4, 7450, 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa']);
$db->prepare('UPDATE flights SET landing_utc=?,air_time_seconds=?,distance_nm=?,max_altitude_ft=? WHERE id=?')
    ->execute([$landingPayload['LandingUTC'], 1800, 50.0, 4200, 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb']);

$owned = nx_user_flight('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'user-a');
expect($owned !== null && $owned['nickname'] === 'C172', 'owner must access flight');
expect(nx_user_flight('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'user-b') === null, 'other user must not access flight');
expect(nx_user_flight('not-a-flight-id', 'user-a') === null, 'invalid identifiers must be rejected');
expect(nx_flight_payload($owned)['ID'] === $payload['ID'], 'stored payload must decode');
expect(count(nx_comparable_landings($owned, 'user-a')) === 1, 'previous landings at the same airport must be comparable');
expect(count(nx_comparable_landings($owned, 'user-b')) === 0, 'ghost landings must remain private to the owner');
$summary = nx_pilot_summary('user-a');
expect($summary['flights'] === 2 && $summary['air_time_seconds'] === 4800, 'pilot summary must aggregate private flight history');
expect($summary['aircraft'] === 1, 'pilot summary must count saved aircraft profiles');
$memory = nx_flight_milestones($owned, 'user-a');
expect($memory['flight_number'] === 2, 'postflight memory must preserve chronological flight number');
expect(count(array_filter($memory['milestones'], static fn(array $item): bool => $item['kind'] === 'distance')) === 1, 'a personal longest route must be recognized');
$db->prepare('INSERT INTO flight_logbook(flight_id,user_id,aircraft_id,role,notes,confirmed,updated_at) VALUES(?,?,?,?,?,?,?)')
    ->execute([$owned['id'], 'user-a', 'aircraft-a', 'PIC', 'Private postflight note', 1, gmdate(DATE_RFC3339)]);
expect((int)$db->query("SELECT confirmed FROM flight_logbook WHERE flight_id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'")->fetchColumn() === 1, 'confirmed logbook draft must persist privately');
expect((int)$db->query("SELECT value FROM schema_meta WHERE key='version'")->fetchColumn() === 5, 'database migration must commit its schema version');

unset($db);
foreach (glob($testDir . '/*') ?: [] as $file) unlink($file);
rmdir($testDir);

echo "Flight Memory tests passed.\n";
