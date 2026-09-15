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
    ],
];

$validTrack = nx_flight_track($payload);
expect(count($validTrack) === 2, 'invalid track points must be removed');
expect($validTrack[0]['AltitudeFt'] === 0.0, 'non-finite metrics must be normalized');
expect(
    nx_downsample_track(range(0, 9), 3) === [0, 5, 9],
    'downsampling must preserve both endpoints'
);
expect(
    nx_flight_filename($payload, 'gpx') === 'stratux-nx-2026-09-14-CYUL-CYQB.gpx',
    'export filename must be stable and safe'
);

$db = nx_db();
$db->prepare('INSERT INTO users(id,email,name,created_at) VALUES(?,?,?,?)')
    ->execute(['user-a', 'pilot@example.com', 'Pilot', gmdate(DATE_RFC3339)]);
$db->prepare('INSERT INTO users(id,email,name,created_at) VALUES(?,?,?,?)')
    ->execute(['user-b', 'other@example.com', 'Other', gmdate(DATE_RFC3339)]);
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

$owned = nx_user_flight('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'user-a');
expect($owned !== null && $owned['nickname'] === 'C172', 'owner must access flight');
expect(nx_user_flight('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'user-b') === null, 'other user must not access flight');
expect(nx_user_flight('not-a-flight-id', 'user-a') === null, 'invalid identifiers must be rejected');
expect(nx_flight_payload($owned)['ID'] === $payload['ID'], 'stored payload must decode');

unset($db);
foreach (glob($testDir . '/*') ?: [] as $file) unlink($file);
rmdir($testDir);

echo "Flight Memory tests passed.\n";
