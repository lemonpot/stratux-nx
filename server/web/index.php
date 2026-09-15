<?php
declare(strict_types=1);
require dirname(__DIR__) . '/lib.php';
require dirname(__DIR__) . '/flight-memory.php';

header('Cache-Control: no-store, no-cache, must-revalidate');
header('Pragma: no-cache');
header('Expires: 0');
header('Referrer-Policy: strict-origin');
header('X-Frame-Options: DENY');

$asset = (string)($_GET['asset'] ?? '');
$assetFiles = [
    'icon' => ['icon.png', 'image/png'],
    'logo-dark' => ['logo-dark.png', 'image/png'],
    'logo-light' => ['logo-light.png', 'image/png'],
    'flight-memory-css' => ['flight-memory.css', 'text/css; charset=utf-8'],
    'flight-memory-js' => ['flight-memory.js', 'application/javascript; charset=utf-8'],
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

if ($action === 'flight-json' || $action === 'flight-export') {
    $apiUser = nx_require_user();
    $flightRow = nx_user_flight((string)($_GET['flight_id'] ?? ''), (string)$apiUser['id']);
    if (!$flightRow) nx_json(['error' => 'flight unavailable'], 404);
    $flightData = [];
    try {
        $flightData = nx_flight_payload($flightRow);
    } catch (Throwable $e) {
        nx_json(['error' => 'flight data unavailable'], 422);
    }
    if ($action === 'flight-json') {
        $validTrack = nx_flight_track($flightData);
        if (count($validTrack) < 2) nx_json(['error' => 'flight track unavailable'], 422);
        $flightData['Track'] = nx_downsample_track($validTrack, 5000);
        nx_json([
            'flight' => $flightData,
            'landing' => nx_landing_analysis($flightData),
            'ghost_landings' => nx_comparable_landings($flightRow, (string)$apiUser['id']),
            'memory' => nx_flight_milestones($flightRow, (string)$apiUser['id']),
            'meta' => [
                'id' => $flightRow['id'],
                'device_name' => $flightRow['nickname'] ?: 'Stratux NX',
            ],
        ]);
    }
    nx_send_flight_export($flightData, strtolower((string)($_GET['format'] ?? '')));
}

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
        $redirect = '/';
        if ($formAction === 'claim') {
            $code = strtoupper(trim((string)($_POST['code'] ?? '')));
            $db = nx_db();
            $db->exec('BEGIN IMMEDIATE');
            try {
                $stmt = $db->prepare(
                    'SELECT c.*,i.user_id,i.previous_user_id,i.revoked_at
                     FROM claim_codes c JOIN installations i ON i.id=c.installation_id
                     WHERE c.code_hash=? AND c.used_at IS NULL AND c.expires_at>?'
                );
                $stmt->execute([hash('sha256', $code), time()]);
                $claim = $stmt->fetch();
                if (!$claim) throw new RuntimeException('The link code is invalid or expired');
                $boundOwner = (string)($claim['user_id'] ?: $claim['previous_user_id']);
                if ($claim['revoked_at']) throw new RuntimeException('This installation key was revoked');
                if ($boundOwner !== '' && $boundOwner !== (string)$user['id']) {
                    throw new RuntimeException('This device is linked to another account');
                }
                $db->prepare('UPDATE installations SET user_id=?,previous_user_id=? WHERE id=?')
                    ->execute([$user['id'], $user['id'], $claim['installation_id']]);
                $db->prepare('UPDATE claim_codes SET used_at=? WHERE code_hash=?')->execute([time(), $claim['code_hash']]);
                $db->exec('COMMIT');
            } catch (Throwable $claimError) {
                if ($db->inTransaction()) $db->exec('ROLLBACK');
                throw $claimError;
            }
            setcookie('nx_pending_claim', '', ['expires' => 1, 'path' => '/', 'secure' => true, 'httponly' => true, 'samesite' => 'Lax']);
        } elseif ($formAction === 'rename') {
            nx_db()->prepare('UPDATE installations SET nickname=? WHERE id=? AND user_id=?')
                ->execute([trim((string)($_POST['nickname'] ?? '')), (string)($_POST['installation_id'] ?? ''), $user['id']]);
        } elseif ($formAction === 'unlink') {
            nx_db()->prepare('UPDATE installations SET user_id=NULL,previous_user_id=?,nickname="" WHERE id=? AND user_id=?')
                ->execute([$user['id'], (string)($_POST['installation_id'] ?? ''), $user['id']]);
        } elseif ($formAction === 'revoke') {
            nx_db()->prepare('UPDATE installations SET revoked_at=?, user_id=NULL WHERE id=? AND user_id=?')
                ->execute([gmdate(DATE_RFC3339), (string)($_POST['installation_id'] ?? ''), $user['id']]);
        } elseif ($formAction === 'delete-flight') {
            nx_db()->prepare('DELETE FROM flights WHERE id=? AND installation_id IN (SELECT id FROM installations WHERE user_id=?)')
                ->execute([(string)($_POST['flight_id'] ?? ''), $user['id']]);
        } elseif ($formAction === 'save-pilot-profile') {
            $displayName = trim(substr((string)($_POST['display_name'] ?? ''), 0, 80));
            $homeAirport = strtoupper(trim((string)($_POST['home_airport'] ?? '')));
            $timezone = trim((string)($_POST['timezone'] ?? 'UTC'));
            if ($displayName === '') throw new RuntimeException('Enter the name you want Stratux NX to use');
            if ($homeAirport !== '' && !preg_match('/^[A-Z0-9]{3,4}$/', $homeAirport)) {
                throw new RuntimeException('Home airport must be a valid 3 or 4 character code');
            }
            if (!in_array($timezone, DateTimeZone::listIdentifiers(), true) && $timezone !== 'UTC') {
                throw new RuntimeException('Select a valid timezone');
            }
            nx_db()->beginTransaction();
            nx_db()->prepare('UPDATE users SET name=? WHERE id=?')->execute([$displayName, $user['id']]);
            nx_db()->prepare('INSERT INTO pilot_profiles(user_id,home_airport,timezone,updated_at) VALUES(?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET home_airport=excluded.home_airport,timezone=excluded.timezone,updated_at=excluded.updated_at')
                ->execute([$user['id'], $homeAirport, $timezone, gmdate(DATE_RFC3339)]);
            nx_db()->commit();
        } elseif ($formAction === 'save-aircraft') {
            $aircraftId = trim((string)($_POST['aircraft_id'] ?? ''));
            $registration = strtoupper(trim(substr((string)($_POST['registration'] ?? ''), 0, 16)));
            $manufacturer = trim(substr((string)($_POST['manufacturer'] ?? ''), 0, 64));
            $model = trim(substr((string)($_POST['model'] ?? ''), 0, 64));
            $nickname = trim(substr((string)($_POST['aircraft_nickname'] ?? ''), 0, 64));
            if ($registration !== '' && !preg_match('/^[A-Z0-9-]+$/', $registration)) {
                throw new RuntimeException('Registration may contain only letters, numbers and hyphens');
            }
            if ($registration === '' && $model === '' && $nickname === '') {
                throw new RuntimeException('Add a registration, model or aircraft name');
            }
            $now = gmdate(DATE_RFC3339);
            if ($aircraftId === '') {
                nx_db()->prepare('INSERT INTO aircraft_profiles(id,user_id,registration,manufacturer,model,nickname,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)')
                    ->execute([nx_uuid(), $user['id'], $registration, $manufacturer, $model, $nickname, $now, $now]);
            } else {
                nx_db()->prepare('UPDATE aircraft_profiles SET registration=?,manufacturer=?,model=?,nickname=?,updated_at=? WHERE id=? AND user_id=?')
                    ->execute([$registration, $manufacturer, $model, $nickname, $now, $aircraftId, $user['id']]);
            }
        } elseif ($formAction === 'delete-aircraft') {
            $aircraftId = (string)($_POST['aircraft_id'] ?? '');
            nx_db()->beginTransaction();
            nx_db()->prepare('UPDATE installations SET aircraft_id=NULL WHERE aircraft_id=? AND user_id=?')
                ->execute([$aircraftId, $user['id']]);
            nx_db()->prepare('UPDATE flights SET aircraft_id=NULL WHERE aircraft_id=? AND installation_id IN (SELECT id FROM installations WHERE user_id=?)')
                ->execute([$aircraftId, $user['id']]);
            nx_db()->prepare('UPDATE flight_logbook SET aircraft_id=NULL WHERE aircraft_id=? AND user_id=?')
                ->execute([$aircraftId, $user['id']]);
            nx_db()->prepare('DELETE FROM aircraft_profiles WHERE id=? AND user_id=?')
                ->execute([$aircraftId, $user['id']]);
            nx_db()->commit();
        } elseif ($formAction === 'assign-aircraft') {
            $installationId = (string)($_POST['installation_id'] ?? '');
            $aircraftId = trim((string)($_POST['aircraft_id'] ?? ''));
            if ($aircraftId !== '') {
                $stmt = nx_db()->prepare('SELECT 1 FROM aircraft_profiles WHERE id=? AND user_id=?');
                $stmt->execute([$aircraftId, $user['id']]);
                if (!$stmt->fetchColumn()) throw new RuntimeException('Aircraft profile not found');
            }
            nx_db()->prepare('UPDATE installations SET aircraft_id=? WHERE id=? AND user_id=?')
                ->execute([$aircraftId !== '' ? $aircraftId : null, $installationId, $user['id']]);
        } elseif ($formAction === 'save-logbook') {
            $flightId = (string)($_POST['flight_id'] ?? '');
            $ownedFlight = nx_user_flight($flightId, (string)$user['id']);
            if (!$ownedFlight) throw new RuntimeException('Flight not found');
            $role = (string)($_POST['role'] ?? '');
            $roles = ['', 'PIC', 'SIC', 'Dual received', 'Instructor', 'Passenger', 'Other'];
            if (!in_array($role, $roles, true)) throw new RuntimeException('Select a valid flight role');
            $notes = trim(substr((string)($_POST['notes'] ?? ''), 0, 4000));
            $aircraftId = trim((string)($_POST['aircraft_id'] ?? ''));
            if ($aircraftId !== '') {
                $stmt = nx_db()->prepare('SELECT 1 FROM aircraft_profiles WHERE id=? AND user_id=?');
                $stmt->execute([$aircraftId, $user['id']]);
                if (!$stmt->fetchColumn()) throw new RuntimeException('Aircraft profile not found');
            }
            $confirmed = isset($_POST['confirmed']) ? 1 : 0;
            nx_db()->beginTransaction();
            nx_db()->prepare('INSERT INTO flight_logbook(flight_id,user_id,aircraft_id,role,notes,confirmed,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(flight_id) DO UPDATE SET aircraft_id=excluded.aircraft_id,role=excluded.role,notes=excluded.notes,confirmed=excluded.confirmed,updated_at=excluded.updated_at WHERE user_id=excluded.user_id')
                ->execute([$flightId, $user['id'], $aircraftId !== '' ? $aircraftId : null, $role, $notes, $confirmed, gmdate(DATE_RFC3339)]);
            nx_db()->prepare('UPDATE flights SET aircraft_id=? WHERE id=? AND installation_id IN (SELECT id FROM installations WHERE user_id=?)')
                ->execute([$aircraftId !== '' ? $aircraftId : null, $flightId, $user['id']]);
            nx_db()->commit();
            $redirect = '/?page=flight&flight_id=' . rawurlencode($flightId);
        }
        header('Location: ' . $redirect);
        exit;
    } catch (Throwable $e) {
        if (nx_db()->inTransaction()) nx_db()->rollBack();
        $error = $e->getMessage();
    }
}

$devices = [];
$flights = [];
$flightDetail = null;
$flightPayload = null;
$pilotProfile = ['home_airport' => '', 'timezone' => 'UTC'];
$aircraftProfiles = [];
$pilotSummary = ['flights' => 0, 'air_time_seconds' => 0, 'distance_nm' => 0, 'aircraft' => 0, 'airports' => 0];
$flightMilestones = null;
$logbook = ['aircraft_id' => '', 'role' => '', 'notes' => '', 'confirmed' => 0];
if ($user) {
    $stmt = nx_db()->prepare('SELECT i.*, a.registration aircraft_registration, a.model aircraft_model, a.nickname aircraft_nickname, COUNT(f.id) flight_count FROM installations i LEFT JOIN aircraft_profiles a ON a.id=i.aircraft_id AND a.user_id=i.user_id LEFT JOIN flights f ON f.installation_id=i.id WHERE i.user_id=? GROUP BY i.id ORDER BY i.created_at DESC');
    $stmt->execute([$user['id']]);
    $devices = $stmt->fetchAll();
    $stmt = nx_db()->prepare('SELECT * FROM pilot_profiles WHERE user_id=?');
    $stmt->execute([$user['id']]);
    $pilotProfile = $stmt->fetch() ?: $pilotProfile;
    $stmt = nx_db()->prepare('SELECT * FROM aircraft_profiles WHERE user_id=? ORDER BY created_at DESC');
    $stmt->execute([$user['id']]);
    $aircraftProfiles = $stmt->fetchAll();
    $pilotSummary = nx_pilot_summary((string)$user['id']);
    $stmt = nx_db()->prepare('SELECT f.id, f.off_block_utc, f.departure_code, f.arrival_code, f.created_at, i.nickname, a.registration aircraft_registration, a.model aircraft_model, a.nickname aircraft_nickname FROM flights f JOIN installations i ON i.id=f.installation_id LEFT JOIN aircraft_profiles a ON a.id=f.aircraft_id AND a.user_id=i.user_id WHERE i.user_id=? ORDER BY COALESCE(NULLIF(f.off_block_utc,""),f.created_at) DESC LIMIT 250');
    $stmt->execute([$user['id']]);
    $flights = $stmt->fetchAll();
    if ($page === 'flight') {
        $flightDetail = nx_user_flight((string)($_GET['flight_id'] ?? ''), (string)$user['id']);
        if ($flightDetail) {
            try {
                $flightPayload = nx_flight_payload($flightDetail);
                $flightMilestones = nx_flight_milestones($flightDetail, (string)$user['id']);
                $stmt = nx_db()->prepare('SELECT * FROM flight_logbook WHERE flight_id=? AND user_id=?');
                $stmt->execute([$flightDetail['id'], $user['id']]);
                $savedLogbook = $stmt->fetch();
                if ($savedLogbook) {
                    $logbook = $savedLogbook;
                } else {
                    $logbook['aircraft_id'] = (string)($flightDetail['aircraft_id'] ?? '');
                }
            } catch (Throwable $e) {
                $error = 'This flight record cannot be opened';
            }
        } else {
            $error = 'This flight is unavailable';
        }
    }
}

function h(string $value): string { return htmlspecialchars($value, ENT_QUOTES, 'UTF-8'); }
function duration_text(int $seconds): string {
    if ($seconds <= 0) return '—';
    $hours = intdiv($seconds, 3600);
    $minutes = intdiv($seconds % 3600, 60);
    return $hours > 0 ? $hours . 'h ' . $minutes . 'm' : $minutes . 'm';
}
function metric(float|int $value, string $suffix = ''): string {
    return number_format((float)$value, (float)$value === floor((float)$value) ? 0 : 1) . $suffix;
}
function flight_time_text(string $value, bool $date = false): string {
    $timestamp = strtotime($value);
    if ($timestamp === false) return '—';
    return gmdate($date ? 'M j, Y · H:i \U\T\C' : 'H:i \U\T\C', $timestamp);
}
function aircraft_label(array $row): string {
    $nickname = trim((string)($row['aircraft_nickname'] ?? ''));
    if ($nickname !== '') return $nickname;
    $identity = trim((string)($row['aircraft_registration'] ?? '') . ' ' . (string)($row['aircraft_model'] ?? ''));
    return $identity !== '' ? $identity : (string)(($row['nickname'] ?? '') ?: 'Stratux NX');
}
?>
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="robots" content="noindex,nofollow">
  <link rel="icon" type="image/png" href="/?asset=icon">
  <title>Stratux NX Account</title>
  <?php if ($page === 'flight' && $user): ?>
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/cesium@1.145.0/Build/Cesium/Widgets/widgets.css" integrity="sha384-ghEeMdcWWzRv/BPeUcX835vcKDGrxvROXisl/Btpv3GeekBUXTSPVcFJpI1Tcrgp" crossorigin="anonymous">
  <?php endif ?>
  <?php if ($user): ?><link rel="stylesheet" href="/?asset=flight-memory-css"><?php endif ?>
  <style>
    :root{--bg:#f6f8fb;--card:#fff;--text:#1a1f36;--muted:#697386;--border:#e3e8ee;--primary:#635bff;--soft:#f0efff;--danger:#b4234d}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.top{height:68px;background:var(--card);border-bottom:1px solid var(--border);display:flex;align-items:center;justify-content:space-between;padding:0 28px}.brand-logo{display:block;width:178px;height:auto}.wrap{max-width:1100px;margin:0 auto;padding:36px 24px}.hero{margin-bottom:24px}.hero h1{font-size:28px;margin:0 0 6px}.hero p,.muted{color:var(--muted)}.card{background:var(--card);border:1px solid var(--border);border-radius:8px;margin:14px 0;overflow:hidden}.head{padding:16px 18px;border-bottom:1px solid var(--border)}.head h2{font-size:16px;margin:0}.body{padding:18px}.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.device,.flight{border:1px solid var(--border);border-radius:7px;padding:14px}.device strong,.flight strong{display:block}.row{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.row.space{justify-content:space-between}.btn{display:inline-flex;align-items:center;justify-content:center;min-height:36px;padding:0 13px;border:1px solid var(--border);border-radius:6px;background:var(--card);color:var(--text);font:600 13px inherit;text-decoration:none;cursor:pointer}.btn.primary{background:var(--primary);border-color:var(--primary);color:#fff}.btn.danger{color:var(--danger)}input{height:36px;border:1px solid var(--border);border-radius:6px;padding:0 10px;min-width:210px;background:var(--card);color:var(--text)}.notice{padding:12px 14px;border-radius:6px;background:#fff3f6;color:var(--danger);margin-bottom:16px}.login{max-width:480px;margin:80px auto}.login .btn{width:100%;margin-top:10px}.pill{display:inline-block;padding:3px 7px;border-radius:99px;background:var(--soft);color:#5147e5;font-size:11px;font-weight:600}.empty{padding:22px;color:var(--muted);text-align:center}.footer{display:flex;justify-content:center;gap:16px;margin-top:32px;color:var(--muted);font-size:12px}.footer a{color:inherit}@media(prefers-color-scheme:dark){:root{--bg:#07131f;--card:#0b1b2a;--text:#f6f9fc;--muted:#9eacba;--border:#213449;--soft:#17263c}.pill{color:#a9a4ff}.notice{background:#321826}}@media(max-width:700px){.grid{grid-template-columns:1fr}.top{padding:0 16px}.brand-logo{width:154px}.wrap{padding:24px 16px}.row.space{align-items:flex-start;flex-direction:column}input{width:100%}}
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
    <h2>Replay maps</h2><p>Opening a 3D flight replay requests map tiles directly from OpenStreetMap. OpenStreetMap receives the Internet address of your browser and the tile coordinates needed to draw the viewed area. Stratux NX does not send your account identity to OpenStreetMap. Map tiles are covered by the <a href="https://wiki.osmfoundation.org/wiki/Privacy_Policy">OpenStreetMap Foundation privacy policy</a>.</p>
    <h2>Device and account data</h2><p>Each installation uses a random identifier and cryptographic key. If you link a device, its synchronized flights become associated with your Google or Apple account. We receive the account identifier, email, display name and optional profile image supplied by that provider. Stratux NX never receives your Google or Apple password.</p>
    <h2>Use and retention</h2><p>Data is used only to provide flight history, device linking and synchronization. It is not sold. Cloud flights can be deleted individually, and devices can be unlinked or revoked from the account page. Local copies remain on the receiver until removed there.</p>
    <h2>Security and contact</h2><p>Device uploads are signed and sent over HTTPS. No Internet service can guarantee absolute security. For access, deletion or privacy questions, contact <a href="mailto:tomas@lemonpot.com">tomas@lemonpot.com</a>.</p>
    <p><a class="btn" href="/">Back to Stratux NX</a></p>
  </div></section>
<?php elseif ($page === 'legal'): ?>
  <section class="hero"><h1>Legal and open-source notices</h1><p>Stratux NX is an independent distribution maintained by Lemonpot.</p></section>
  <section class="card"><div class="body">
    <h2>Open-source foundation</h2>
    <p>Stratux NX combines Lemonpot-authored modifications with the community-maintained <a href="https://github.com/stratux/stratux">Stratux project</a>, which continues the original work founded by Christopher Young and other contributors. Stratux NX is not an official upstream release, and upstream names are not used to imply endorsement.</p>
    <h2>Licenses and source</h2>
    <p>Lemonpot-authored Stratux NX files are available under the BSD 3-Clause License unless stated otherwise. Stratux and bundled components remain under their respective licenses, including GPL-licensed receiver components. Copyright notices, component licenses and corresponding source are available in the <a href="https://github.com/lemonpot/stratux-nx">Stratux NX repository and releases</a>.</p>
    <h2>No warranty and aviation use</h2>
    <p>The software and services are provided without warranty to the extent permitted by their applicable terms. Stratux NX provides supplemental situational awareness only. It is not certified avionics and must not be used as the sole source for navigation, traffic avoidance, weather decisions or flight safety.</p>
    <p><a class="btn" href="/">Back to Stratux NX</a></p>
  </div></section>
<?php elseif ($page === 'flight' && $user && $flightDetail && $flightPayload): ?>
  <?php
    $departure = (string)($flightPayload['DepartureAirport']['Code'] ?? $flightDetail['departure_code'] ?? '---');
    $arrival = (string)($flightPayload['ArrivalAirport']['Code'] ?? $flightDetail['arrival_code'] ?? '---');
    $flightEndpoint = '/?' . http_build_query(['action' => 'flight-json', 'flight_id' => $flightDetail['id']]);
    $exportBase = '/?' . http_build_query(['action' => 'flight-export', 'flight_id' => $flightDetail['id']]);
  ?>
  <a class="memory-back" href="/">← All flights</a>
  <section class="memory-hero">
    <div>
      <h1 class="memory-route"><?=h($departure)?> → <?=h($arrival)?></h1>
      <p class="memory-subtitle"><?=h(flight_time_text((string)($flightPayload['OffBlockUTC'] ?? $flightDetail['created_at']), true))?> · <?=h(aircraft_label($flightDetail))?></p>
    </div>
    <div class="memory-hero-stats">
      <div class="memory-hero-stat"><small>Air time</small><strong><?=h(duration_text((int)($flightPayload['AirTimeSeconds'] ?? 0)))?></strong></div>
      <div class="memory-hero-stat"><small>Distance</small><strong><?=h(metric((float)($flightPayload['DistanceNM'] ?? 0), ' NM'))?></strong></div>
    </div>
  </section>

  <div class="memory-summary">
    <div><span>Block time</span><strong><?=h(duration_text((int)($flightPayload['BlockTimeSeconds'] ?? 0)))?></strong></div>
    <div><span>Maximum altitude</span><strong><?=h(metric((float)($flightPayload['MaxAltitudeFt'] ?? 0), ' ft'))?></strong></div>
    <div><span>Maximum groundspeed</span><strong><?=h(metric((float)($flightPayload['MaxGroundSpeedKt'] ?? 0), ' kt'))?></strong></div>
    <div><span>Touch-and-go</span><strong><?=intval($flightPayload['TouchAndGoCount'] ?? 0)?></strong></div>
  </div>

  <section class="card postflight-card">
    <div class="head row space"><div><h2>Postflight memory</h2><span class="muted">Flight <?=intval($flightMilestones['flight_number'] ?? 1)?> in your private history.</span></div><span class="pill"><?=h(duration_text((int)($flightMilestones['cumulative_air_time_seconds'] ?? 0)))?> remembered</span></div>
    <div class="body">
      <p class="postflight-lead">You recorded <?=h(metric((float)($flightPayload['DistanceNM'] ?? 0), ' NM'))?> from <?=h($departure)?> to <?=h($arrival)?> with <?=h(duration_text((int)($flightPayload['AirTimeSeconds'] ?? 0)))?> airborne.</p>
      <div class="milestone-grid">
        <?php foreach (($flightMilestones['milestones'] ?? []) as $milestone): ?>
          <div class="milestone"><span>Personal milestone</span><strong><?=h($milestone['title'])?></strong><p><?=h($milestone['detail'])?></p></div>
        <?php endforeach ?>
        <?php if (empty($flightMilestones['milestones'])): ?><div class="milestone is-quiet"><span>Flight Memory</span><strong>Another flight remembered</strong><p>This flight now contributes to your private trends and future comparisons.</p></div><?php endif ?>
      </div>
    </div>
  </section>

  <section class="card logbook-card">
    <div class="head row space"><div><h2>Logbook draft</h2><span class="muted">Review the automatically recorded times, then keep your private notes.</span></div><span class="pill <?=$logbook['confirmed'] ? 'is-confirmed' : ''?>"><?=$logbook['confirmed'] ? 'Confirmed' : 'Draft'?></span></div>
    <div class="body">
      <div class="logbook-record">
        <div><span>Date</span><strong><?=h(flight_time_text((string)($flightPayload['OffBlockUTC'] ?? ''), true))?></strong></div>
        <div><span>Route</span><strong><?=h($departure . ' → ' . $arrival)?></strong></div>
        <div><span>Air time</span><strong><?=h(duration_text((int)($flightPayload['AirTimeSeconds'] ?? 0)))?></strong></div>
        <div><span>Landings observed</span><strong><?=1 + intval($flightPayload['TouchAndGoCount'] ?? 0)?></strong></div>
      </div>
      <form class="logbook-form" method="post">
        <input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>">
        <input type="hidden" name="form_action" value="save-logbook">
        <input type="hidden" name="flight_id" value="<?=h($flightDetail['id'])?>">
        <label><span>Aircraft</span><select name="aircraft_id"><option value="">Not assigned</option><?php foreach ($aircraftProfiles as $aircraft): ?><option value="<?=h($aircraft['id'])?>" <?=$logbook['aircraft_id'] === $aircraft['id'] ? 'selected' : ''?>><?=h($aircraft['nickname'] ?: trim($aircraft['registration'] . ' ' . $aircraft['model']))?></option><?php endforeach ?></select></label>
        <label><span>Role</span><select name="role"><?php foreach (['', 'PIC', 'SIC', 'Dual received', 'Instructor', 'Passenger', 'Other'] as $role): ?><option value="<?=h($role)?>" <?=$logbook['role'] === $role ? 'selected' : ''?>><?=h($role ?: 'Not specified')?></option><?php endforeach ?></select></label>
        <label class="logbook-notes"><span>Private notes</span><textarea name="notes" rows="4" maxlength="4000" placeholder="What would you like to remember about this flight?"><?=h($logbook['notes'])?></textarea></label>
        <label class="logbook-confirm"><input type="checkbox" name="confirmed" value="1" <?=$logbook['confirmed'] ? 'checked' : ''?>><span>I reviewed this draft and confirmed it for my personal records.</span></label>
        <button class="btn primary">Save logbook</button>
      </form>
      <p class="profile-note">Stratux NX estimates times and landings from recorded sensor data. Verify entries before using them in any official or regulatory logbook.</p>
    </div>
  </section>

  <div id="flight-memory" data-flight-endpoint="<?=h($flightEndpoint)?>">
    <section class="card memory-viewer-card">
      <div class="head memory-viewer-head">
        <div><h2>3D Flight Replay</h2><span class="muted">Follow the complete flight in time and altitude.</span></div>
        <div id="replay-status" class="replay-status is-info" role="status">Loading flight…</div>
      </div>
      <div class="replay-stage">
        <p class="memory-sr-only">Interactive route replay from <?=h($departure)?> to <?=h($arrival)?>. Use Play or the timeline slider to move through the flight.</p>
        <div id="replay-3d" aria-label="3D flight replay"></div>
        <div id="replay-fallback" hidden><canvas id="replay-fallback-canvas" role="img" aria-label="Recorded route from <?=h($departure)?> to <?=h($arrival)?>"></canvas></div>
        <div class="replay-attribution"><a href="https://www.openstreetmap.org/copyright" rel="noopener">© OpenStreetMap contributors</a></div>
      </div>
      <div class="replay-controls">
        <div class="replay-live-stats">
          <div><span>Phase</span><strong id="replay-current-phase">—</strong></div>
          <div><span>Time</span><strong id="replay-current-time">—</strong></div>
          <div><span>Altitude</span><strong id="replay-current-altitude">—</strong></div>
          <div><span>Groundspeed</span><strong id="replay-current-speed">—</strong></div>
          <div><span>Vertical speed</span><strong id="replay-current-vs">—</strong></div>
        </div>
        <div class="replay-transport">
          <button id="replay-play" class="btn primary" type="button" disabled>Play</button>
          <input id="replay-scrubber" type="range" min="0" max="1" value="0" disabled aria-label="Flight replay position">
          <select id="replay-speed" disabled aria-label="Replay speed">
            <option value="1">1×</option>
            <option value="5">5×</option>
            <option value="20" selected>20×</option>
            <option value="60">60×</option>
            <option value="120">120×</option>
          </select>
          <button id="replay-follow" class="btn" type="button" disabled>Follow aircraft</button>
          <button id="replay-traffic" class="btn" type="button" hidden>Show nearby traffic</button>
        </div>
      </div>
    </section>

    <section class="card profile-card">
      <div class="head"><h2>Vertical profile</h2><span class="muted">Altitude recorded throughout the flight.</span></div>
      <div class="body">
        <p class="memory-sr-only">The flight reached a maximum recorded altitude of <?=h(metric((float)($flightPayload['MaxAltitudeFt'] ?? 0), ' feet'))?>.</p>
        <div class="profile-canvas-wrap"><canvas id="flight-profile-canvas" role="img" aria-label="Flight altitude profile"></canvas></div>
        <p class="profile-note">GPS altitude is supplemental and may differ from indicated or pressure altitude.</p>
      </div>
    </section>

    <section id="landing-signature" class="card landing-card" hidden>
      <div class="head memory-viewer-head">
        <div><h2>Landing Signature</h2><span class="muted">A GPS-derived view of the final approach and touchdown.</span></div>
        <span id="landing-confidence" class="landing-confidence">Confidence unavailable</span>
      </div>
      <div class="body">
        <div class="landing-metrics">
          <div><span>Estimated touchdown</span><strong id="landing-touchdown-time">—</strong></div>
          <div><span>Vertical speed</span><strong id="landing-touchdown-vs">—</strong></div>
          <div><span>Groundspeed</span><strong id="landing-touchdown-speed">—</strong></div>
          <div><span>Final-minute average</span><strong id="landing-average-vs">—</strong></div>
        </div>
        <div class="landing-chart-wrap">
          <canvas id="landing-signature-canvas" role="img" aria-label="Final approach altitude and groundspeed profile"></canvas>
        </div>
        <div class="landing-detail-grid">
          <div>
            <h3>What the receiver observed</h3>
            <ul id="landing-observations" class="landing-observations"></ul>
          </div>
          <div class="landing-caution">
            <h3>Interpret with care</h3>
            <ul id="landing-limitations" class="landing-observations"></ul>
          </div>
        </div>
      </div>
    </section>

    <section id="ghost-landing" class="card ghost-card" hidden>
      <div class="head">
        <h2>Ghost Landing</h2>
        <span class="muted">Compare this approach with your previous arrivals at <?=h($arrival)?> from the same device.</span>
      </div>
      <div class="body">
        <div id="ghost-empty" class="ghost-empty" hidden>Your next arrival here will unlock a private comparison.</div>
        <div id="ghost-content" hidden>
          <div class="ghost-chart-wrap">
            <canvas id="ghost-landing-canvas" role="img" aria-label="Comparison of current and previous approach profiles"></canvas>
          </div>
          <div id="ghost-comparisons" class="ghost-comparisons"></div>
          <p class="profile-note">Compared by time to estimated touchdown. Same airport does not guarantee the same runway or conditions.</p>
        </div>
      </div>
    </section>

    <section id="context-journey" class="card context-card" hidden>
      <div class="head"><h2>Cockpit Context</h2><span class="muted">What Stratux NX could see and support throughout this flight.</span></div>
      <div class="body">
        <div class="landing-metrics context-metrics">
          <div><span>GPS available</span><strong id="context-gps">—</strong></div>
          <div><span>ADS-B reception</span><strong id="context-reception">—</strong></div>
          <div><span>Peak nearby traffic</span><strong id="context-traffic">—</strong></div>
          <div><span>Internet available</span><strong id="context-internet">—</strong></div>
        </div>
        <div class="context-chart-wrap"><canvas id="context-journey-canvas" role="img" aria-label="Flight reception, traffic and GPS context timeline"></canvas></div>
        <p class="profile-note">Context snapshots are recorded periodically. Gaps mean the receiver did not record that context, not necessarily that the service was unavailable.</p>
      </div>
    </section>

    <section id="weather-journey" class="card weather-card" hidden>
      <div class="head"><h2>Weather Journey</h2><span class="muted">Weather products actually received by this Stratux NX during the flight.</span></div>
      <div class="body">
        <div id="weather-summary" class="weather-summary"></div>
        <div id="weather-reports" class="weather-reports"></div>
        <p class="profile-note">Received weather is not a reconstruction of exact conditions at the aircraft and does not replace an official briefing.</p>
      </div>
    </section>
  </div>

  <section class="card">
    <div class="head"><h2>Flight timeline</h2></div>
    <div class="body memory-phase-line">
      <div class="memory-phase"><span>Off block</span><strong><?=h(flight_time_text((string)($flightPayload['OffBlockUTC'] ?? '')))?></strong></div>
      <div class="memory-phase"><span>Takeoff</span><strong><?=h(flight_time_text((string)($flightPayload['TakeoffUTC'] ?? '')))?></strong></div>
      <div class="memory-phase"><span>Landing</span><strong><?=h(flight_time_text((string)($flightPayload['LandingUTC'] ?? '')))?></strong></div>
      <div class="memory-phase"><span>On block</span><strong><?=h(flight_time_text((string)($flightPayload['OnBlockUTC'] ?? '')))?></strong></div>
    </div>
  </section>

  <section class="card">
    <div class="head"><h2>Take your flight with you</h2></div>
    <div class="body memory-actions">
      <span class="label">Export the original recorded track:</span>
      <a class="btn" href="<?=h($exportBase . '&format=gpx')?>">Download GPX</a>
      <a class="btn" href="<?=h($exportBase . '&format=kml')?>">Download KML</a>
      <a class="btn" href="<?=h($exportBase . '&format=csv')?>">Download CSV</a>
    </div>
  </section>
<?php elseif (!$user): ?>
  <section class="login card"><div class="head"><h2><?= $claimCode !== '' ? 'Link your Stratux NX' : 'Your Stratux NX flights' ?></h2></div><div class="body"><p class="muted"><?= $claimCode !== '' ? 'Sign in to securely link the device that sent you here. The one-time code is already included.' : 'Sign in to link one or more Stratux NX devices and see flights they have synchronized.' ?></p><?php if (nx_env('GOOGLE_CLIENT_ID') !== '' && nx_env('GOOGLE_CLIENT_SECRET') !== ''): ?><a class="btn primary" href="/?action=login-google<?= $claimCode !== '' ? '&amp;claim=' . rawurlencode($claimCode) : '' ?>">Continue with Google</a><?php endif ?><?php if (nx_env('APPLE_CLIENT_ID') !== '' && nx_env('APPLE_CLIENT_SECRET') !== ''): ?><a class="btn" href="/?action=login-apple<?= $claimCode !== '' ? '&amp;claim=' . rawurlencode($claimCode) : '' ?>">Continue with Apple</a><?php endif ?><?php if (nx_env('GOOGLE_CLIENT_ID') === '' && nx_env('APPLE_CLIENT_ID') === ''): ?><p class="muted">Account login is being configured.</p><?php endif ?><p class="muted"><a href="/?page=privacy">Privacy policy</a></p></div></section>
<?php else: ?>
  <section class="hero"><h1>Flight history</h1><p><?=h($user['email'])?> · <?=count($devices)?> linked device<?=count($devices) === 1 ? '' : 's'?></p></section>
  <section class="pilot-memory-summary">
    <div><span>Flights</span><strong><?=intval($pilotSummary['flights'])?></strong></div>
    <div><span>Air time</span><strong><?=h(duration_text((int)$pilotSummary['air_time_seconds']))?></strong></div>
    <div><span>Distance</span><strong><?=h(metric((float)$pilotSummary['distance_nm'], ' NM'))?></strong></div>
    <div><span>Airports</span><strong><?=intval($pilotSummary['airports'])?></strong></div>
    <div><span>Aircraft</span><strong><?=intval($pilotSummary['aircraft'])?></strong></div>
  </section>
  <section class="card pilot-dna-card"><div class="head row space"><div><h2>Pilot DNA</h2><span class="muted">Private patterns that grow from your own flight history.</span></div><span class="pill">Only you</span></div><div class="body pilot-dna-grid">
    <div><span>Last 30 days</span><strong><?=intval($pilotSummary['recent_flights'] ?? 0)?> flight<?=intval($pilotSummary['recent_flights'] ?? 0) === 1 ? '' : 's'?></strong><p>Your recent flying rhythm.</p></div>
    <div><span>Most visited</span><strong><?=h(($pilotSummary['top_airport'] ?? '') ?: 'Not enough history')?></strong><p>Based on recorded departures and arrivals.</p></div>
    <div><span>Average flight</span><strong><?=h(duration_text((int)($pilotSummary['average_air_time_seconds'] ?? 0)))?></strong><p>Average recorded airborne time.</p></div>
    <div><span>Most flown aircraft</span><strong><?=h(($pilotSummary['top_aircraft'] ?? '') ?: 'Assign an aircraft')?></strong><p>Uses confirmed aircraft assignments.</p></div>
  </div></section>
  <section class="card"><div class="head"><h2><?= $claimCode !== '' ? 'Confirm device link' : 'Link a Stratux NX' ?></h2></div><div class="body"><form class="row" method="post"><input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>"><input type="hidden" name="form_action" value="claim"><input name="code" value="<?=h($claimCode)?>" placeholder="ABCD-2345" maxlength="9" required><button class="btn primary"><?= $claimCode !== '' ? 'Link this device' : 'Link device' ?></button><span class="muted"><?= $claimCode !== '' ? 'The secure one-time code was supplied by your Stratux NX.' : 'Generate the one-time code from Flight Log on the device.' ?></span></form></div></section>
  <section class="card"><div class="head"><h2>Pilot profile</h2></div><div class="body">
    <form class="profile-form" method="post">
      <input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>">
      <input type="hidden" name="form_action" value="save-pilot-profile">
      <label><span>Name</span><input name="display_name" value="<?=h((string)$user['name'])?>" maxlength="80" required></label>
      <label><span>Home airport</span><input name="home_airport" value="<?=h((string)$pilotProfile['home_airport'])?>" maxlength="4" placeholder="CYUL"></label>
      <label><span>Timezone</span><select name="timezone">
        <?php foreach (array_values(array_unique(array_merge(['UTC'], DateTimeZone::listIdentifiers()))) as $timezone): ?>
          <option value="<?=h($timezone)?>" <?=$pilotProfile['timezone'] === $timezone ? 'selected' : ''?>><?=h(str_replace('_', ' ', $timezone))?></option>
        <?php endforeach ?>
      </select></label>
      <button class="btn primary">Save pilot profile</button>
    </form>
  </div></section>
  <section class="card"><div class="head row space"><div><h2>Aircraft</h2><span class="muted">Keep flight history organized by the aircraft you fly.</span></div><span class="pill"><?=count($aircraftProfiles)?> saved</span></div><div class="body aircraft-grid">
    <form class="aircraft-profile is-new" method="post">
      <input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>">
      <input type="hidden" name="form_action" value="save-aircraft">
      <strong>Add aircraft</strong>
      <div class="aircraft-fields">
        <label><span>Registration</span><input name="registration" maxlength="16" placeholder="C-GABC"></label>
        <label><span>Manufacturer</span><input name="manufacturer" maxlength="64" placeholder="Cessna"></label>
        <label><span>Model</span><input name="model" maxlength="64" placeholder="172M"></label>
        <label><span>Name</span><input name="aircraft_nickname" maxlength="64" placeholder="Optional nickname"></label>
      </div>
      <button class="btn primary">Add aircraft</button>
    </form>
    <?php foreach ($aircraftProfiles as $aircraft): ?>
      <form class="aircraft-profile" method="post">
        <input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>">
        <input type="hidden" name="aircraft_id" value="<?=h($aircraft['id'])?>">
        <strong><?=h($aircraft['nickname'] ?: ($aircraft['registration'] ?: ($aircraft['model'] ?: 'Aircraft')))?></strong>
        <div class="aircraft-fields">
          <label><span>Registration</span><input name="registration" value="<?=h($aircraft['registration'])?>" maxlength="16"></label>
          <label><span>Manufacturer</span><input name="manufacturer" value="<?=h($aircraft['manufacturer'])?>" maxlength="64"></label>
          <label><span>Model</span><input name="model" value="<?=h($aircraft['model'])?>" maxlength="64"></label>
          <label><span>Name</span><input name="aircraft_nickname" value="<?=h($aircraft['nickname'])?>" maxlength="64"></label>
        </div>
        <div class="row"><button class="btn" name="form_action" value="save-aircraft">Save</button><button class="btn danger" name="form_action" value="delete-aircraft" onclick="return confirm('Delete this aircraft profile? Existing flights remain stored.')">Delete</button></div>
      </form>
    <?php endforeach ?>
  </div></section>
  <section class="card"><div class="head"><h2>Devices</h2></div><div class="body grid">
    <?php foreach ($devices as $device): ?>
      <div class="device">
        <div class="row space"><div><strong><?=h($device['nickname'] ?: 'Stratux NX')?></strong><span class="muted"><?=h($device['id'])?></span></div><span class="pill"><?=intval($device['flight_count'])?> flights</span></div>
        <p class="muted">Last seen <?=h($device['last_seen_at'])?> · <?=h($device['software_version'])?></p>
        <form class="device-form" method="post">
          <input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>">
          <input type="hidden" name="installation_id" value="<?=h($device['id'])?>">
          <label><span>Device name</span><input name="nickname" value="<?=h($device['nickname'])?>" placeholder="My Stratux NX"></label>
          <button class="btn" name="form_action" value="rename">Save name</button>
        </form>
        <form class="device-form" method="post">
          <input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>">
          <input type="hidden" name="installation_id" value="<?=h($device['id'])?>">
          <label><span>Assigned aircraft</span><select name="aircraft_id"><option value="">Not assigned</option><?php foreach ($aircraftProfiles as $aircraft): ?><option value="<?=h($aircraft['id'])?>" <?=$device['aircraft_id'] === $aircraft['id'] ? 'selected' : ''?>><?=h($aircraft['nickname'] ?: trim($aircraft['registration'] . ' ' . $aircraft['model']))?></option><?php endforeach ?></select></label>
          <button class="btn" name="form_action" value="assign-aircraft">Assign</button>
        </form>
        <div class="row device-danger-actions">
          <form method="post"><input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>"><input type="hidden" name="installation_id" value="<?=h($device['id'])?>"><button class="btn" name="form_action" value="unlink" onclick="return confirm('Unlink this device? Its cloud flights will remain stored but hidden until it is linked again.')">Unlink</button></form>
          <form method="post"><input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>"><input type="hidden" name="installation_id" value="<?=h($device['id'])?>"><button class="btn danger" name="form_action" value="revoke" onclick="return confirm('Permanently revoke this installation key? This device will no longer be able to upload until reinstalled.')">Revoke</button></form>
        </div>
      </div>
    <?php endforeach ?>
    <?php if (!$devices): ?><div class="empty">No linked devices yet.</div><?php endif ?>
  </div></section>
  <section class="card"><div class="head"><h2>Synced flights</h2></div><div class="body grid">
    <?php foreach ($flights as $flight): ?><div class="flight flight-list-card"><div class="row space"><div><strong><?=h(($flight['departure_code'] ?: '---') . ' → ' . ($flight['arrival_code'] ?: '---'))?></strong><span class="muted"><?=h(flight_time_text($flight['off_block_utc'] ?: $flight['created_at'], true))?></span></div><span class="pill"><?=h(aircraft_label($flight))?></span></div><a class="btn primary flight-open" href="/?page=flight&amp;flight_id=<?=rawurlencode($flight['id'])?>">Open Flight Memory</a><form method="post"><input type="hidden" name="csrf" value="<?=h(nx_csrf_token())?>"><input type="hidden" name="flight_id" value="<?=h($flight['id'])?>"><button class="btn danger" name="form_action" value="delete-flight" onclick="return confirm('Delete this cloud copy? The local copy on the device is not removed.')">Delete cloud copy</button></form></div><?php endforeach ?>
    <?php if (!$flights): ?><div class="empty">No synchronized flights yet.</div><?php endif ?>
  </div></section>
<?php endif ?>
<footer class="footer"><a href="/?page=privacy">Privacy</a><a href="/?page=legal">Legal</a><a href="https://github.com/lemonpot/stratux-nx">Source</a></footer>
</main>
<?php if ($page === 'flight' && $user && $flightDetail && $flightPayload): ?>
  <script>window.CESIUM_BASE_URL='https://cdn.jsdelivr.net/npm/cesium@1.145.0/Build/Cesium/';</script>
  <script src="https://cdn.jsdelivr.net/npm/cesium@1.145.0/Build/Cesium/Cesium.js" integrity="sha384-D1oR8FyBDsJWkPeydGJTh8nJg5/++9sqchzJuu+oGQPmgbwu3aJmoVj3BTowr6t6" crossorigin="anonymous"></script>
  <script src="/?asset=flight-memory-js"></script>
<?php endif ?>
</body>
</html>
