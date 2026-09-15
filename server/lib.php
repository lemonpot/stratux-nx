<?php
declare(strict_types=1);

define('NX_DATA_DIR', getenv('NX_DATA_DIR') ?: '/home/virtual/lemonpot/stratuxnx/data');
define('NX_DB_PATH', NX_DATA_DIR . '/stratuxnx.sqlite');
define('NX_APP_URL', getenv('NX_APP_URL') ?: 'https://app.stratuxnx.com');

$nxConfigFile = __DIR__ . '/config/app.php';
if (is_file($nxConfigFile)) {
    $nxConfig = require $nxConfigFile;
    if (is_array($nxConfig)) {
        foreach ($nxConfig as $key => $value) {
            if (is_string($key) && is_scalar($value) && getenv($key) === false) putenv($key . '=' . (string)$value);
        }
    }
}

function nx_db(): PDO {
    static $db;
    if ($db instanceof PDO) return $db;
    if (!is_dir(NX_DATA_DIR)) mkdir(NX_DATA_DIR, 0750, true);
    $db = new PDO('sqlite:' . NX_DB_PATH, null, null, [
        PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION,
        PDO::ATTR_DEFAULT_FETCH_MODE => PDO::FETCH_ASSOC,
    ]);
    $db->exec('PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000');
    nx_schema($db);
    return $db;
}

function nx_schema(PDO $db): void {
    $db->exec(<<<'SQL'
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL DEFAULT '',
  avatar_url TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS oauth_identities (
  provider TEXT NOT NULL,
  subject TEXT NOT NULL,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY(provider, subject)
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS pilot_profiles (
  user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  home_airport TEXT NOT NULL DEFAULT '',
  timezone TEXT NOT NULL DEFAULT 'UTC',
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS aircraft_profiles (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  registration TEXT NOT NULL DEFAULT '',
  manufacturer TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  nickname TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS installations (
  id TEXT PRIMARY KEY,
  public_key TEXT NOT NULL,
  user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  previous_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  nickname TEXT NOT NULL DEFAULT '',
  software_version TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  revoked_at TEXT,
  aircraft_id TEXT REFERENCES aircraft_profiles(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS flights (
  id TEXT PRIMARY KEY,
  installation_id TEXT NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
  aircraft_id TEXT REFERENCES aircraft_profiles(id) ON DELETE SET NULL,
  client_flight_id TEXT NOT NULL,
  payload TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  off_block_utc TEXT NOT NULL DEFAULT '',
  landing_utc TEXT NOT NULL DEFAULT '',
  departure_code TEXT NOT NULL DEFAULT '',
  arrival_code TEXT NOT NULL DEFAULT '',
  air_time_seconds INTEGER NOT NULL DEFAULT 0,
  distance_nm REAL NOT NULL DEFAULT 0,
  max_altitude_ft REAL NOT NULL DEFAULT 0,
  metrics_backfilled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  UNIQUE(installation_id, client_flight_id)
);
CREATE TABLE IF NOT EXISTS claim_codes (
  code_hash TEXT PRIMARY KEY,
  installation_id TEXT NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL,
  used_at INTEGER
);
CREATE TABLE IF NOT EXISTS flight_logbook (
  flight_id TEXT PRIMARY KEY REFERENCES flights(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  aircraft_id TEXT REFERENCES aircraft_profiles(id) ON DELETE SET NULL,
  role TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  confirmed INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS request_limits (
  subject TEXT NOT NULL,
  bucket INTEGER NOT NULL,
  request_count INTEGER NOT NULL,
  PRIMARY KEY(subject, bucket)
);
CREATE TABLE IF NOT EXISTS schema_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_installations_user ON installations(user_id);
CREATE INDEX IF NOT EXISTS idx_flights_installation ON flights(installation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_aircraft_profiles_user ON aircraft_profiles(user_id, created_at DESC);
SQL);
    $db->exec('BEGIN IMMEDIATE');
    try {
        $versionStmt = $db->prepare("SELECT value FROM schema_meta WHERE key='version'");
        $versionStmt->execute();
        $schemaVersion = (int)($versionStmt->fetchColumn() ?: 0);

        $columns = array_column($db->query('PRAGMA table_info(flights)')->fetchAll(), 'name');
        $flightColumns = [
            'content_hash' => "TEXT NOT NULL DEFAULT ''",
            'aircraft_id' => 'TEXT',
            'landing_utc' => "TEXT NOT NULL DEFAULT ''",
            'air_time_seconds' => 'INTEGER NOT NULL DEFAULT 0',
            'distance_nm' => 'REAL NOT NULL DEFAULT 0',
            'max_altitude_ft' => 'REAL NOT NULL DEFAULT 0',
            'metrics_backfilled' => 'INTEGER NOT NULL DEFAULT 0',
        ];
        foreach ($flightColumns as $name => $definition) {
            if (!in_array($name, $columns, true)) $db->exec("ALTER TABLE flights ADD COLUMN $name $definition");
        }

        $installationColumns = array_column($db->query('PRAGMA table_info(installations)')->fetchAll(), 'name');
        $installationAdditions = ['revoked_at' => 'TEXT', 'aircraft_id' => 'TEXT', 'previous_user_id' => 'TEXT'];
        foreach ($installationAdditions as $name => $definition) {
            if (!in_array($name, $installationColumns, true)) $db->exec("ALTER TABLE installations ADD COLUMN $name $definition");
        }

        if ($schemaVersion < 3) {
            $db->exec('UPDATE installations SET previous_user_id=user_id WHERE previous_user_id IS NULL AND user_id IS NOT NULL');
        }
        $db->prepare("INSERT INTO schema_meta(key,value) VALUES('version',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value")
            ->execute(['3']);
        $db->exec('COMMIT');
    } catch (Throwable $error) {
        if ($db->inTransaction()) $db->exec('ROLLBACK');
        throw $error;
    }
    nx_backfill_flight_metrics($db);
}

function nx_backfill_flight_metrics(PDO $db): void {
    $rows = $db->query('SELECT id,payload FROM flights WHERE metrics_backfilled=0')->fetchAll();
    if (!$rows) return;
    $update = $db->prepare('UPDATE flights SET landing_utc=?,air_time_seconds=?,distance_nm=?,max_altitude_ft=?,metrics_backfilled=1 WHERE id=? AND metrics_backfilled=0');
    foreach ($rows as $row) {
        $flight = json_decode((string)$row['payload'], true);
        if (!is_array($flight)) {
            $update->execute(['', 0, 0, 0, $row['id']]);
            continue;
        }
        $distance = (float)($flight['DistanceNM'] ?? 0);
        $altitude = (float)($flight['MaxAltitudeFt'] ?? 0);
        $update->execute([
            substr((string)($flight['LandingUTC'] ?? ''), 0, 40),
            min(604800, max(0, (int)($flight['AirTimeSeconds'] ?? 0))),
            is_finite($distance) ? min(30000, max(0, $distance)) : 0,
            is_finite($altitude) ? min(100000, max(0, $altitude)) : 0,
            $row['id'],
        ]);
    }
}

function nx_json(array $payload, int $status = 200): never {
    $body = json_encode($payload, JSON_UNESCAPED_SLASHES);
    if ($body === false) {
        $status = 500;
        $body = '{"error":"response encoding failed"}';
    }
    http_response_code($status);
    header('Content-Type: application/json; charset=utf-8');
    header('Cache-Control: no-store');
    header('X-Content-Type-Options: nosniff');
    echo $body;
    exit;
}

function nx_body(int $maxBytes = 10485760): string {
    $length = (int)($_SERVER['CONTENT_LENGTH'] ?? 0);
    if ($length > $maxBytes) nx_json(['error' => 'request too large'], 413);
    $body = file_get_contents('php://input', false, null, 0, $maxBytes + 1);
    if ($body === false || strlen($body) > $maxBytes) nx_json(['error' => 'request too large'], 413);
    return $body;
}

function nx_require_method(string $method): void {
    if (($_SERVER['REQUEST_METHOD'] ?? '') !== $method) nx_json(['error' => "$method required"], 405);
}

function nx_uuid(): string {
    $b = random_bytes(16);
    $b[6] = chr((ord($b[6]) & 0x0f) | 0x40);
    $b[8] = chr((ord($b[8]) & 0x3f) | 0x80);
    return vsprintf('%s%s-%s-%s-%s-%s%s%s', str_split(bin2hex($b), 4));
}

function nx_b64url_decode(string $value): string|false {
    $padding = strlen($value) % 4;
    if ($padding) $value .= str_repeat('=', 4 - $padding);
    return base64_decode(strtr($value, '-_', '+/'), true);
}

function nx_device_headers(): array {
    return [
        trim((string)($_SERVER['HTTP_X_INSTALLATION_ID'] ?? '')),
        trim((string)($_SERVER['HTTP_X_TIMESTAMP'] ?? '')),
        trim((string)($_SERVER['HTTP_X_SIGNATURE'] ?? '')),
    ];
}

function nx_verify_device(string $body, ?string $registrationPublicKey = null): array {
    [$installationId, $timestamp, $signatureText] = nx_device_headers();
    if (!preg_match('/^NX-[A-F0-9-]{36}$/', $installationId)) nx_json(['error' => 'invalid installation id'], 401);
    if (!ctype_digit($timestamp) || abs(time() - (int)$timestamp) > 300) nx_json(['error' => 'expired request'], 401);

    $db = nx_db();
    $stmt = $db->prepare('SELECT * FROM installations WHERE id = ?');
    $stmt->execute([$installationId]);
    $installation = $stmt->fetch();
    $publicKeyText = $installation['public_key'] ?? $registrationPublicKey;
    $publicKey = nx_b64url_decode((string)$publicKeyText);
    $signature = nx_b64url_decode($signatureText);
    if ($publicKey === false || strlen($publicKey) !== SODIUM_CRYPTO_SIGN_PUBLICKEYBYTES ||
        $signature === false || strlen($signature) !== SODIUM_CRYPTO_SIGN_BYTES ||
        !sodium_crypto_sign_verify_detached($signature, $timestamp . "\n" . $body, $publicKey)) {
        nx_json(['error' => 'invalid device signature'], 401);
    }
    if ($installation && !hash_equals($installation['public_key'], (string)$publicKeyText)) {
        nx_json(['error' => 'installation key mismatch'], 409);
    }
    if ($installation && !empty($installation['revoked_at'])) nx_json(['error' => 'installation access revoked'], 403);
    nx_rate_limit('device:' . $installationId, 120);
    return [$installationId, $installation];
}

function nx_rate_limit(string $subject, int $limit): void {
    $db = nx_db();
    $bucket = intdiv(time(), 60);
    $db->prepare('INSERT INTO request_limits(subject,bucket,request_count) VALUES(?,?,1) ON CONFLICT(subject,bucket) DO UPDATE SET request_count=request_count+1')
        ->execute([$subject, $bucket]);
    $stmt = $db->prepare('SELECT request_count FROM request_limits WHERE subject=? AND bucket=?');
    $stmt->execute([$subject, $bucket]);
    if ((int)$stmt->fetchColumn() > $limit) nx_json(['error' => 'rate limit exceeded'], 429);
    if (random_int(1, 100) === 1) $db->prepare('DELETE FROM request_limits WHERE bucket<?')->execute([$bucket - 2]);
}

function nx_claim_code(): string {
    $alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
    $raw = '';
    for ($i = 0; $i < 8; $i++) $raw .= $alphabet[random_int(0, strlen($alphabet) - 1)];
    return substr($raw, 0, 4) . '-' . substr($raw, 4);
}

function nx_session_user(): ?array {
    $token = (string)($_COOKIE['nx_session'] ?? '');
    if ($token === '') return null;
    $stmt = nx_db()->prepare('SELECT u.* FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?');
    $stmt->execute([hash('sha256', $token), time()]);
    return $stmt->fetch() ?: null;
}

function nx_require_user(): array {
    $user = nx_session_user();
    if (!$user) nx_json(['error' => 'authentication required'], 401);
    return $user;
}

function nx_create_session(string $userId): void {
    $token = bin2hex(random_bytes(32));
    $expires = time() + 60 * 60 * 24 * 30;
    nx_db()->prepare('INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)')
        ->execute([hash('sha256', $token), $userId, $expires]);
    setcookie('nx_session', $token, [
        'expires' => $expires, 'path' => '/', 'secure' => true,
        'httponly' => true, 'samesite' => 'Lax',
    ]);
}

function nx_csrf_token(): string {
    $session = (string)($_COOKIE['nx_session'] ?? '');
    $secret = nx_env('NX_CSRF_SECRET');
    if ($secret === '') $secret = hash('sha256', __FILE__ . php_uname());
    return hash_hmac('sha256', $session, $secret);
}

function nx_require_csrf(): void {
    $provided = (string)($_POST['csrf'] ?? '');
    if ($provided === '' || !hash_equals(nx_csrf_token(), $provided)) {
        http_response_code(403);
        exit('Invalid request token');
    }
}

function nx_upsert_oauth_user(string $provider, string $subject, string $email, string $name, string $avatar = ''): array {
    if ($subject === '' || $email === '') throw new RuntimeException('Identity provider did not return an email');
    $db = nx_db();
    $db->beginTransaction();
    try {
        $stmt = $db->prepare('SELECT u.* FROM oauth_identities o JOIN users u ON u.id=o.user_id WHERE o.provider=? AND o.subject=?');
        $stmt->execute([$provider, $subject]);
        $user = $stmt->fetch();
        if (!$user) {
            $stmt = $db->prepare('SELECT * FROM users WHERE email=?');
            $stmt->execute([strtolower($email)]);
            $user = $stmt->fetch();
            if (!$user) {
                $user = ['id' => nx_uuid(), 'email' => strtolower($email), 'name' => $name, 'avatar_url' => $avatar];
                $db->prepare('INSERT INTO users(id,email,name,avatar_url,created_at) VALUES(?,?,?,?,?)')
                    ->execute([$user['id'], $user['email'], $name, $avatar, gmdate(DATE_RFC3339)]);
            }
            $db->prepare('INSERT INTO oauth_identities(provider,subject,user_id) VALUES(?,?,?)')
                ->execute([$provider, $subject, $user['id']]);
        }
        $db->commit();
        return $user;
    } catch (Throwable $e) {
        $db->rollBack();
        throw $e;
    }
}

function nx_env(string $name): string {
    return trim((string)(getenv($name) ?: ''));
}

function nx_http_form(string $url, array $fields): array {
    $ch = curl_init($url);
    curl_setopt_array($ch, [
        CURLOPT_POST => true,
        CURLOPT_POSTFIELDS => http_build_query($fields),
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT => 15,
        CURLOPT_HTTPHEADER => ['Accept: application/json'],
    ]);
    $body = curl_exec($ch);
    $status = (int)curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    if ($body === false || $status < 200 || $status >= 300) throw new RuntimeException('Identity provider request failed');
    $decoded = json_decode($body, true);
    if (!is_array($decoded)) throw new RuntimeException('Invalid identity provider response');
    return $decoded;
}

function nx_http_bearer_json(string $url, string $token): array {
    $ch = curl_init($url);
    curl_setopt_array($ch, [
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT => 15,
        CURLOPT_HTTPHEADER => ['Accept: application/json', 'Authorization: Bearer ' . $token],
    ]);
    $body = curl_exec($ch);
    $status = (int)curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    if ($body === false || $status < 200 || $status >= 300) throw new RuntimeException('Identity provider profile request failed');
    $decoded = json_decode($body, true);
    if (!is_array($decoded)) throw new RuntimeException('Invalid identity provider profile');
    return $decoded;
}

function nx_oauth_state(string $provider): string {
    $state = bin2hex(random_bytes(24));
    setcookie('nx_oauth_' . $provider, hash('sha256', $state), [
        'expires' => time() + 600, 'path' => '/', 'secure' => true,
        'httponly' => true, 'samesite' => $provider === 'apple' ? 'None' : 'Lax',
    ]);
    return $state;
}

function nx_validate_oauth_state(string $provider, string $state): void {
    $expected = (string)($_COOKIE['nx_oauth_' . $provider] ?? '');
    if ($expected === '' || !hash_equals($expected, hash('sha256', $state))) throw new RuntimeException('Invalid OAuth state');
    setcookie('nx_oauth_' . $provider, '', ['expires' => 1, 'path' => '/', 'secure' => true, 'httponly' => true, 'samesite' => $provider === 'apple' ? 'None' : 'Lax']);
}

function nx_jwt_payload(string $jwt): array {
    $parts = explode('.', $jwt);
    if (count($parts) !== 3) throw new RuntimeException('Invalid identity token');
    $payload = json_decode((string)nx_b64url_decode($parts[1]), true);
    if (!is_array($payload) || (int)($payload['exp'] ?? 0) < time()) throw new RuntimeException('Expired identity token');
    return $payload;
}
