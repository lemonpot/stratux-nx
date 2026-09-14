<?php
declare(strict_types=1);
require dirname(__DIR__) . '/lib.php';

header('Cache-Control: no-store, no-cache, must-revalidate');
header('Pragma: no-cache');
header('Expires: 0');

$asset = (string)($_GET['asset'] ?? '');
$assetFiles = [
    'icon' => ['icon.png', 'image/png'],
    'logo-dark' => ['logo-dark.png', 'image/png'],
    'logo-light' => ['logo-light.png', 'image/png'],
];
if (isset($assetFiles[$asset])) {
    [$assetFile, $contentType] = $assetFiles[$asset];
    header('Content-Type: ' . $contentType);
    header('X-Content-Type-Options: nosniff');
    readfile(__DIR__ . '/' . $assetFile);
    exit;
}

$action = (string)($_GET['action'] ?? '');
$page = (string)($_GET['page'] ?? '');
$claimCode = strtoupper(trim((string)($_GET['claim'] ?? $_COOKIE['nx_pending_claim'] ?? '')));
$error = '';

if ($claimCode !== '') {
    if (preg_match('/^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$/', $claimCode)) {
        setcookie('nx_pending_claim', $claimCode, [
            'expires' => time() + 900, 'path' => '/', 'secure' => true,
            'httponly' => true, 'samesite' => 'Lax',
        ]);
    } else {
        $claimCode = '';
        $error = 'The device link code is invalid';
    }
}

try {
    if ($action === 'login-google') {
        $clientId = nx_env('GOOGLE_CLIENT_ID');
        if ($clientId === '') throw new RuntimeException('Google login is not configured yet');
        $state = nx_oauth_state('google');
        $query = http_build_query([
            'client_id' => $clientId,
            'redirect_uri' => NX_APP_URL . '/?action=google-callback',
            'response_type' => 'code',
            'scope' => 'openid email profile',
            'state' => $state,
            'prompt' => 'select_account',
        ]);
        header('Location: https://accounts.google.com/o/oauth2/v2/auth?' . $query);
        exit;
    }

    if ($action === 'google-callback') {
        nx_validate_oauth_state('google', (string)($_GET['state'] ?? ''));
        $token = nx_http_form('https://oauth2.googleapis.com/token', [
            'code' => (string)($_GET['code'] ?? ''),
            'client_id' => nx_env('GOOGLE_CLIENT_ID'),
            'client_secret' => nx_env('GOOGLE_CLIENT_SECRET'),
            'redirect_uri' => NX_APP_URL . '/?action=google-callback',
            'grant_type' => 'authorization_code',
        ]);
        $profile = nx_http_bearer_json('https://openidconnect.googleapis.com/v1/userinfo', (string)($token['access_token'] ?? ''));
        $user = nx_upsert_oauth_user('google', (string)($profile['sub'] ?? ''), (string)($profile['email'] ?? ''), (string)($profile['name'] ?? ''), (string)($profile['picture'] ?? ''));
        nx_create_session($user['id']);
        $pendingClaim = (string)($_COOKIE['nx_pending_claim'] ?? '');
        header('Location: /' . ($pendingClaim !== '' ? '?claim=' . rawurlencode($pendingClaim) : ''));
        exit;
    }

    if ($action === 'login-apple') {
        $clientId = nx_env('APPLE_CLIENT_ID');
        if ($clientId === '') throw new RuntimeException('Apple login is not configured yet');
        $state = nx_oauth_state('apple');
        $query = http_build_query([
            'client_id' => $clientId,
            'redirect_uri' => NX_APP_URL . '/?action=apple-callback',
            'response_type' => 'code',
            'response_mode' => 'form_post',
            'scope' => 'name email',
            'state' => $state,
        ]);
        header('Location: https://appleid.apple.com/auth/authorize?' . $query);
        exit;
    }

    if ($action === 'apple-callback') {
        nx_validate_oauth_state('apple', (string)($_POST['state'] ?? ''));
        $token = nx_http_form('https://appleid.apple.com/auth/token', [
            'code' => (string)($_POST['code'] ?? ''),
            'client_id' => nx_env('APPLE_CLIENT_ID'),
            'client_secret' => nx_env('APPLE_CLIENT_SECRET'),
            'redirect_uri' => NX_APP_URL . '/?action=apple-callback',
            'grant_type' => 'authorization_code',
        ]);
        $identity = nx_jwt_payload((string)($token['id_token'] ?? ''));
        if (($identity['aud'] ?? '') !== nx_env('APPLE_CLIENT_ID')) throw new RuntimeException('Invalid Apple identity audience');
        $postedUser = json_decode((string)($_POST['user'] ?? '{}'), true) ?: [];
        $name = trim((string)($postedUser['name']['firstName'] ?? '') . ' ' . (string)($postedUser['name']['lastName'] ?? ''));
        $user = nx_upsert_oauth_user('apple', (string)($identity['sub'] ?? ''), (string)($identity['email'] ?? ''), $name);
        nx_create_session($user['id']);
        $pendingClaim = (string)($_COOKIE['nx_pending_claim'] ?? '');
        header('Location: /' . ($pendingClaim !== '' ? '?claim=' . rawurlencode($pendingClaim) : ''));
        exit;
    }

    if ($action === 'logout') {
        $token = (string)($_COOKIE['nx_session'] ?? '');
        if ($token !== '') nx_db()->prepare('DELETE FROM sessions WHERE token_hash=?')->execute([hash('sha256', $token)]);
        setcookie('nx_session', '', ['expires' => 1, 'path' => '/', 'secure' => true, 'httponly' => true, 'samesite' => 'Lax']);
        header('Location: /');
        exit;
    }
} catch (Throwable $e) {
    $error = $e->getMessage();
}

$user = nx_session_user();
if ($user && ($_SERVER['REQUEST_METHOD'] ?? '') === 'POST') {
    try {
        nx_require_csrf();
        $formAction = (string)($_POST['form_action'] ?? '');
        if ($formAction === 'claim') {
            $code = strtoupper(trim((string)($_POST['code'] ?? '')));
            $stmt = nx_db()->prepare('SELECT * FROM claim_codes WHERE code_hash=? AND used_at IS NULL AND expires_at>?');
            $stmt->execute([hash('sha256', $code), time()]);
            $claim = $stmt->fetch();
            if (!$claim) throw new RuntimeException('The link code is invalid or expired');
            nx_db()->beginTransaction();
            nx_db()->prepare('UPDATE installations SET user_id=? WHERE id=?')->execute([$user['id'], $claim['installation_id']]);
            nx_db()->prepare('UPDATE claim_codes SET used_at=? WHERE code_hash=?')->execute([time(), $claim['code_hash']]);
            nx_db()->commit();
            setcookie('nx_pending_claim', '', ['expires' => 1, 'path' => '/', 'secure' => true, 'httponly' => true, 'samesite' => 'Lax']);
        } elseif ($formAction === 'rename') {
            nx_db()->prepare('UPDATE installations SET nickname=? WHERE id=? AND user_id=?')
                ->execute([trim((string)($_POST['nickname'] ?? '')), (string)($_POST['installation_id'] ?? ''), $user['id']]);
        } elseif ($formAction === 'unlink') {
            nx_db()->prepare('UPDATE installations SET user_id=NULL, nickname="" WHERE id=? AND user_id=?')
                ->execute([(string)($_POST['installation_id'] ?? ''), $user['id']]);
        } elseif ($formAction === 'revoke') {
            nx_db()->prepare('UPDATE installations SET revoked_at=?, user_id=NULL WHERE id=? AND user_id=?')
                ->execute([gmdate(DATE_RFC3339), (string)($_POST['installation_id'] ?? ''), $user['id']]);
        } elseif ($formAction === 'delete-flight') {
            nx_db()->prepare('DELETE FROM flights WHERE id=? AND installation_id IN (SELECT id FROM installations WHERE user_id=?)')
                ->execute([(string)($_POST['flight_id'] ?? ''), $user['id']]);
        }
        header('Location: /');
        exit;
    } catch (Throwable $e) {
        if (nx_db()->inTransaction()) nx_db()->rollBack();
        $error = $e->getMessage();
    }
}

$devices = [];
$flights = [];
if ($user) {
    $stmt = nx_db()->prepare('SELECT i.*, COUNT(f.id) flight_count FROM installations i LEFT JOIN flights f ON f.installation_id=i.id WHERE i.user_id=? GROUP BY i.id ORDER BY i.created_at DESC');
    $stmt->execute([$user['id']]);
    $devices = $stmt->fetchAll();
    $stmt = nx_db()->prepare('SELECT f.*, i.nickname FROM flights f JOIN installations i ON i.id=f.installation_id WHERE i.user_id=? ORDER BY COALESCE(NULLIF(f.off_block_utc,""),f.created_at) DESC LIMIT 250');
    $stmt->execute([$user['id']]);
    $flights = $stmt->fetchAll();
}

function h(string $value): string { return htmlspecialchars($value, ENT_QUOTES, 'UTF-8'); }
?>
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="robots" content="noindex,nofollow">
  <link rel="icon" type="image/png" href="/?asset=icon">
  <title>Stratux NX Account</title>
  <style>
    :root{--bg:#f6f8fb;--card:#fff;--text:#1a1f36;--muted:#697386;--border:#e3e8ee;--primary:#635bff;--soft:#f0efff;--danger:#b4234d}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.top{height:68px;background:var(--card);border-bottom:1px solid var(--border);display:flex;align-items:center;justify-content:space-between;padding:0 28px}.brand-logo{display:block;width:178px;height:auto}.wrap{max-width:1100px;margin:0 auto;padding:36px 24px}.hero{margin-bottom:24px}.hero h1{font-size:28px;margin:0 0 6px}.hero p,.muted{color:var(--muted)}.card{background:var(--card);border:1px solid var(--border);border-radius:8px;margin:14px 0;overflow:hidden}.head{padding:16px 18px;border-bottom:1px solid var(--border)}.head h2{font-size:16px;margin:0}.body{padding:18px}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.device,.flight{border:1px solid var(--border);border-radius:7px;padding:14px}.device strong,.flight strong{display:block}.row{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.row.space{justify-content:space-between}.btn{display:inline-flex;align-items:center;justify-content:center;min-height:36px;padding:0 13px;border:1px solid var(--border);border-radius:6px;background:var(--card);color:var(--text);font:600 13px inherit;text-decoration:none;cursor:pointer}.btn.primary{background:var(--primary);border-color:var(--primary);color:#fff}.btn.danger{color:var(--danger)}input{height:36px;border:1px solid var(--border);border-radius:6px;padding:0 10px;min-width:210px;background:var(--card);color:var(--text)}.notice{padding:12px 14px;border-radius:6px;background:#fff3f6;color:var(--danger);margin-bottom:16px}.login{max-width:480px;margin:80px auto}.login .btn{width:100%;margin-top:10px}.pill{display:inline-block;padding:3px 7px;border-radius:99px;background:var(--soft);color:#5147e5;font-size:11px;font-weight:600}.empty{padding:22px;color:var(--muted);text-align:center}@media(prefers-color-scheme:dark){:root{--bg:#07131f;--card:#0b1b2a;--text:#f6f9fc;--muted:#9eacba;--border:#213449;--soft:#17263c}.pill{color:#a9a4ff}.notice{background:#321826}}@media(max-width:700px){.grid{grid-template-columns:1fr}.top{padding:0 16px}.brand-logo{width:154px}.wrap{padding:24px 16px}.row.space{align-items:flex-start;flex-direction:column}input{width:100%}}
  </style>
</head>
<body>
<header class="top"><a href="/" aria-label="Stratux NX home"><picture><source media="(prefers-color-scheme: dark)" srcset="/?asset=logo-dark"><img class="brand-logo" src="/?asset=logo-light" alt="Stratux NX"></picture></a><?php if ($user): ?><a class="btn" href="/?action=logout">Sign out</a><?php endif ?></header>
<main class="wrap">
<?php if ($error): ?><div class="notice"><?=h($error)?></div><?php endif ?>
<?php if ($page === 'privacy'): ?>
  <section class="hero"><h1>Privacy policy</h1><p>Last updated September 14, 2026</p></section>
  <section class="card"><div class="body">
    <h2>Flight data</h2><p>Stratux NX records flights locally on your receiver. Cloud synchronization is optional and requires you to enable it on the device. When enabled, completed routes, times, airport information and flight metrics are transmitted securely to Stratux NX servers whenever Internet is available.</p>
    <h2>Device and account data</h2><p>Each installation uses a random identifier and cryptographic key. If you link a device, its synchronized flights become associated with your Google or Apple account. We receive the account identifier, email, display name and optional profile image supplied by that provider. Stratux NX never receives your Google or Apple password.</p>
    <h2>Use and retention</h2><p>Data is used only to provide flight history, device linking and synchronization. It is not sold. Cloud flights can be deleted individually, and devices can be unlinked or revoked from the account page. Local copies remain on the receiver until removed there.</p>
    <h2>Security and contact</h2><p>Device uploads are signed and sent over HTTPS. No Internet service can guarantee absolute security. For access, deletion or privacy questions, contact <a href="mailto:tomas@lemonpot.com">tomas@lemonpot.com</a>.</p>
    <p><a class="btn" href="/">Back to Stratux NX</a></p>
  </div></section>
<?php elseif (!$user): ?>
  <section class="login card"><div class="head"><h2><?= $claimCode !== '' ? 'Link your Stratux NX' : 'Your Stratux NX flights' ?></h2></div><div class="body"><p class="muted"><?= $claimCode !== '' ? 'Sign in to securely link the device that sent you here. The one-time code is already included.' : 'Sign in to link one or more Stratux NX devices and see flights they have synchronized.' ?></p><?php if (nx_env('GOOGLE_CLIENT_ID') !== '' && nx_env('GOOGLE_CLIENT_SECRET') !== ''): ?><a class="btn primary" href="/?action=login-google<?= $claimCode !== '' ? '&amp;claim=' . rawurlencode($claimCode) : '' ?>">Continue with Google</a><?php endif ?><?php if (nx_env('APPLE_CLIENT_ID') !== '' && nx_env('APPLE_CLIENT_SECRET') !== ''): ?><a class="btn" href="/?action=login-apple<?= $claimCode !== '' ? '&amp;claim=' . rawurlencode($claimCode) : '' ?>">Continue with Apple</a><?php endif ?><?php if (nx_env('GOOGLE_CLIENT_ID') === '' && nx_env('APPLE_CLIENT_ID') === ''): ?><p class="muted">Account login is being configured.</p><?php endif ?><p class="muted"><a href="/?page=privacy">Privacy policy</a></p></div></section>
<?php else: ?>
  <section class="hero"><h1>Flight history</h1><p><?=h($user['email'])?> · <?=count($devices)?> linked device<?=count($devices) === 1 ? '' : 's'?></p></section>
  <section class="card"><div class="head"><h2><?= $claimCode !== '' ? 'Confirm device link' : 'Link a Stratux NX' ?></h2></div><div class="body"><form class="row" method="post"><input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>"><input type="hidden" name="form_action" value="claim"><input name="code" value="<?=h($claimCode)?>" placeholder="ABCD-2345" maxlength="9" required><button class="btn primary"><?= $claimCode !== '' ? 'Link this device' : 'Link device' ?></button><span class="muted"><?= $claimCode !== '' ? 'The secure one-time code was supplied by your Stratux NX.' : 'Generate the one-time code from Flight Log on the device.' ?></span></form></div></section>
  <section class="card"><div class="head"><h2>Devices</h2></div><div class="body grid">
    <?php foreach ($devices as $device): ?><div class="device"><div class="row space"><div><strong><?=h($device['nickname'] ?: 'Stratux NX')?></strong><span class="muted"><?=h($device['id'])?></span></div><span class="pill"><?=intval($device['flight_count'])?> flights</span></div><p class="muted">Last seen <?=h($device['last_seen_at'])?> · <?=h($device['software_version'])?></p><form class="row" method="post"><input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>"><input type="hidden" name="installation_id" value="<?=h($device['id'])?>"><input name="nickname" value="<?=h($device['nickname'])?>" placeholder="Aircraft or device name"><button class="btn" name="form_action" value="rename">Save</button><button class="btn" name="form_action" value="unlink" onclick="return confirm('Unlink this device? Its cloud flights will remain stored but hidden until it is linked again.')">Unlink</button><button class="btn danger" name="form_action" value="revoke" onclick="return confirm('Permanently revoke this installation key? This device will no longer be able to upload until reinstalled.')">Revoke</button></form></div><?php endforeach ?>
    <?php if (!$devices): ?><div class="empty">No linked devices yet.</div><?php endif ?>
  </div></section>
  <section class="card"><div class="head"><h2>Synced flights</h2></div><div class="body grid">
    <?php foreach ($flights as $flight): ?><div class="flight"><div class="row space"><div><strong><?=h(($flight['departure_code'] ?: '---') . ' → ' . ($flight['arrival_code'] ?: '---'))?></strong><span class="muted"><?=h($flight['off_block_utc'] ?: $flight['created_at'])?></span></div><span class="pill"><?=h($flight['nickname'] ?: 'Stratux NX')?></span></div><form method="post" style="margin-top:12px"><input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>"><input type="hidden" name="flight_id" value="<?=h($flight['id'])?>"><button class="btn danger" name="form_action" value="delete-flight" onclick="return confirm('Delete this cloud copy? The local copy on the device is not removed.')">Delete cloud copy</button></form></div><?php endforeach ?>
    <?php if (!$flights): ?><div class="empty">No synchronized flights yet.</div><?php endif ?>
  </div></section>
<?php endif ?>
</main>
</body>
</html>
