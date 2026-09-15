<?php
declare(strict_types=1);
require dirname(__DIR__, 3) . '/lib.php';

nx_require_method('POST');
$body = nx_body();
[$installationId, $installation] = nx_verify_device($body);
if (!$installation) nx_json(['error' => 'installation not registered'], 404);
$flight = json_decode($body, true);
if (!is_array($flight) || !is_string($flight['ID'] ?? null) || $flight['ID'] === '') {
    nx_json(['error' => 'invalid flight record'], 400);
}
if (!is_array($flight['Track'] ?? null) || count($flight['Track']) < 2) {
    nx_json(['error' => 'flight track requires at least two points'], 400);
}
if (count($flight['Track']) > 25000) {
    nx_json(['error' => 'flight track exceeds 25000 points'], 413);
}

$db = nx_db();
$contentHash = hash('sha256', $body);
$stmt = $db->prepare('SELECT id, content_hash FROM flights WHERE installation_id=? AND client_flight_id=?');
$stmt->execute([$installationId, $flight['ID']]);
$existingFlight = $stmt->fetch();
$remoteId = $existingFlight['id'] ?? null;
$status = 200;
if ($existingFlight && $existingFlight['content_hash'] !== '' && !hash_equals($existingFlight['content_hash'], $contentHash)) {
    nx_json(['error' => 'flight id already exists with different content'], 409);
}
if (!$remoteId) {
    $remoteId = nx_uuid();
    $departure = (string)($flight['DepartureAirport']['Code'] ?? '');
    $arrival = (string)($flight['ArrivalAirport']['Code'] ?? '');
    $db->prepare('INSERT INTO flights(id,installation_id,client_flight_id,payload,content_hash,off_block_utc,departure_code,arrival_code,created_at) VALUES(?,?,?,?,?,?,?,?,?)')
        ->execute([$remoteId, $installationId, $flight['ID'], $body, $contentHash, (string)($flight['OffBlockUTC'] ?? ''), $departure, $arrival, gmdate(DATE_RFC3339)]);
    $status = 201;
}
$db->prepare('UPDATE installations SET last_seen_at=? WHERE id=?')->execute([gmdate(DATE_RFC3339), $installationId]);

nx_json(['status' => 'ok', 'flight_id' => $remoteId], $status);
