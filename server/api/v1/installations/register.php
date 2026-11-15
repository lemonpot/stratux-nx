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

$linkCode = strtoupper(trim((string)($data['link_code'] ?? '')));
if (nx_valid_link_code($linkCode)) {
    $linkHash = nx_link_code_hash($linkCode);
    $db->exec('BEGIN IMMEDIATE');
    try {
        $db->prepare('UPDATE installations SET link_code_hash=? WHERE id=?')->execute([$linkHash, $installationId]);
        $stmt = $db->prepare('SELECT user_id FROM pending_links WHERE code_hash=?');
        $stmt->execute([$linkHash]);
        $pendingUser = (string)($stmt->fetchColumn() ?: '');
        $stmt = $db->prepare('SELECT user_id, revoked_at FROM installations WHERE id=?');
        $stmt->execute([$installationId]);
        $current = $stmt->fetch();
        if ($pendingUser !== '' && empty($current['user_id']) && empty($current['revoked_at'])) {
            nx_link_installation($db, $installationId, $pendingUser);
        }
        if ($pendingUser !== '') $db->prepare('DELETE FROM pending_links WHERE code_hash=?')->execute([$linkHash]);
        $db->exec('COMMIT');
    } catch (Throwable $error) {
        if ($db->inTransaction()) $db->exec('ROLLBACK');
        throw $error;
    }
}

$account = null;
$stmt = $db->prepare('SELECT u.name, u.email FROM installations i LEFT JOIN users u ON u.id=i.user_id WHERE i.id=?');
$stmt->execute([$installationId]);
$owner = $stmt->fetch();
if ($owner && (string)($owner['email'] ?? '') !== '') {
    $email = strtolower(trim((string)$owner['email']));
    [$local, $domain] = array_pad(explode('@', $email, 2), 2, '');
    $maskedEmail = substr($local, 0, 1) . '***' . ($domain !== '' ? '@' . $domain : '');
    $account = [
        'linked' => true,
        'name' => trim((string)($owner['name'] ?? '')),
        'email' => $maskedEmail,
    ];
}

nx_json([
    'status' => 'ok',
    'installation_id' => $installationId,
    'account' => $account ?? ['linked' => false, 'name' => '', 'email' => ''],
], $existing ? 200 : 201);
