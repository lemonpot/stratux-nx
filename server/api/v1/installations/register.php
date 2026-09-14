<?php
declare(strict_types=1);
require dirname(__DIR__, 3) . '/lib.php';

nx_require_method('POST');
$body = nx_body(32768);
$data = json_decode($body, true);
if (!is_array($data)) nx_json(['error' => 'invalid JSON'], 400);

$publicKey = trim((string)($data['public_key'] ?? ''));
[$installationId, $existing] = nx_verify_device($body, $publicKey);
if ($installationId !== (string)($data['installation_id'] ?? '')) nx_json(['error' => 'installation id mismatch'], 400);

$now = gmdate(DATE_RFC3339);
$db = nx_db();
if ($existing) {
    $db->prepare('UPDATE installations SET software_version=?, last_seen_at=? WHERE id=?')
        ->execute([(string)($data['software_version'] ?? ''), $now, $installationId]);
} else {
    $db->prepare('INSERT INTO installations(id,public_key,software_version,created_at,last_seen_at) VALUES(?,?,?,?,?)')
        ->execute([$installationId, $publicKey, (string)($data['software_version'] ?? ''), $now, $now]);
}

nx_json(['status' => 'ok', 'installation_id' => $installationId], $existing ? 200 : 201);
