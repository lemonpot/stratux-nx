<?php
declare(strict_types=1);
require dirname(__DIR__, 3) . '/lib.php';

nx_require_method('POST');
$body = nx_body(4096);
[$installationId, $installation] = nx_verify_device($body);
if (!$installation) nx_json(['error' => 'installation not registered'], 404);

$code = nx_claim_code();
$expires = time() + 600;
$db = nx_db();
$db->prepare('DELETE FROM claim_codes WHERE installation_id=? OR expires_at<?')->execute([$installationId, time()]);
$db->prepare('INSERT INTO claim_codes(code_hash,installation_id,expires_at) VALUES(?,?,?)')
    ->execute([hash('sha256', strtoupper($code)), $installationId, $expires]);
$db->prepare('UPDATE installations SET last_seen_at=? WHERE id=?')
    ->execute([gmdate(DATE_RFC3339), $installationId]);

nx_json(['code' => $code, 'expires_utc' => gmdate(DATE_RFC3339, $expires)]);
