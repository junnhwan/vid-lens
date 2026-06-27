# VidLens 安全模块 - 源码走读

本文档对 VidLens 项目中安全相关的四个核心模块进行逐行走读，包括 JWT 认证、AES-GCM 加密、SSRF 防护与 URL 校验、CORS 配置、以及 Redis 令牌桶限流。

---

## 1. JWT 认证体系

VidLens 的 JWT 认证分为三个层次：令牌生成、令牌解析、中间件拦截。

### 1.1 令牌生成 (`internal/pkg/jwt/jwt.go:19-35`)

```go
func GenerateToken(userID int64, username, role, secret string, expireHours int) (string, error) {
    now := time.Now()
    claims := Claims{
        UserID:   userID,
        Username: username,
        Role:     role,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireHours) * time.Hour)),
            IssuedAt:  jwt.NewNumericDate(now),
            Issuer:    "vidlens",
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(secret))
}
```

**逐行分析：**

- **Claims 结构体：** 自定义的 `Claims` 嵌入了 `jwt.RegisteredClaims`，同时承载业务字段（`UserID`、`Username`、`Role`）和标准字段（`ExpiresAt`、`IssuedAt`）。这种嵌入模式是 Go 中 JWT 库的惯用手法，既保留了标准兼容性，又扩展了业务语义。
- **签名算法：** `jwt.SigningMethodHS256` 使用 HMAC-SHA256，属于对称签名。密钥 `secret` 以 `[]byte` 形式传入，要求签发方和验证方共享同一密钥。这在单体应用中是合理的简化，但在微服务架构下会导致密钥分发难题。
- **Issuer 字段：** `Issuer: "vidlens"` 在解析时用于校验来源，防止其他系统签发的 token 被误接受。

### 1.2 令牌解析 (`internal/pkg/jwt/jwt.go:37-57`)

```go
func ParseToken(tokenString, secret string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(
        tokenString,
        &Claims{},
        func(token *jwt.Token) (interface{}, error) {
            return []byte(secret), nil
        },
        jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
        jwt.WithIssuer("vidlens"),
    )
    if err != nil {
        return nil, err
    }
    claims, ok := token.Claims.(*Claims)
    if !ok || !token.Valid {
        return nil, errors.New("invalid token")
    }
    return claims, nil
}
```

**安全措施：**
- `jwt.WithValidMethods` 白名单限制签名算法为 HS256，防止算法混淆攻击（alg=none 攻击）
- `jwt.WithIssuer("vidlens")` 校验 Issuer 字段，防止外部 token 被接受

### 1.3 中间件拦截 (`internal/middleware/auth.go:12-50`)

```go
func JWTAuth(secret string) gin.HandlerFunc {
    return func(c *gin.Context) {
        authHeader := c.GetHeader("Authorization")
        if authHeader == "" {
            response.Unauthorized(c, "please login first")
            c.Abort()
            return
        }

        parts := strings.SplitN(authHeader, " ", 2)
        if len(parts) != 2 || parts[0] != "Bearer" {
            response.Unauthorized(c, "invalid token format")
            c.Abort()
            return
        }

        claims, err := jwt.ParseToken(parts[1], secret)
        if err != nil {
            response.Unauthorized(c, "token invalid or expired")
            c.Abort()
            return
        }

        c.Set("userID", claims.UserID)
        c.Set("username", claims.Username)
        c.Set("role", claims.Role)
        c.Next()
    }
}
```

**逐行分析：**

- **第 14-18 行 -- 空头检查：** `authHeader == ""` 检查缺失头，直接返回 401。
- **第 20-24 行 -- 格式校验：** `strings.SplitN(authHeader, " ", 2)` 将 `Bearer <token>` 拆为两部分，检查第一部分是否为 `"Bearer"`。这比 `strings.HasPrefix` 更严格——`"BearerXXX"`（无空格）也会被拒绝。
- **第 26-30 行 -- 解析与错误处理：** `jwt.ParseToken` 内部完成了签名验证、过期检查、Issuer 校验等逻辑。所有错误统一返回 `"token invalid or expired"`，这是一种安全实践——不向客户端暴露具体的失败原因（签名错误、过期、格式错误等），避免信息泄露。
- **第 32-36 行 -- Context 传递：** 使用 `c.Set` 将 Claims 写入 Gin 的请求上下文。`c.Next()` 将控制权传递给后续的 handler。错误分支中已调用 `c.Abort()`，所以后续 handler 不会执行。

**辅助函数（`auth.go:42-49`）：**

```go
func GetUserID(c *gin.Context) int64 {
    id, exists := c.Get("userID")
    if !exists {
        return 0
    }
    return id.(int64)
}
```

`GetUserID` 使用 comma-ok 模式安全地读取 Context 值。返回 `0` 作为默认值意味着如果 handler 没有经过 `JWTAuth` 中间件，会得到一个"幽灵用户 ID = 0"。在业务层必须对 `0` 做防御性检查。

---

## 2. AES-GCM 加密体系

VidLens 使用 AES-256-GCM 对敏感数据（如第三方 API Key）进行加密存储。GCM（Galois/Counter Mode）是一种认证加密模式，同时提供机密性和完整性。

### 2.1 Codec 结构与密钥派生 (`internal/pkg/secret/crypto.go:13-45`)

```go
type Codec struct {
    aead cipher.AEAD
}

func NewCodecFromPassphrase(passphrase string) (*Codec, error) {
    if passphrase == "" {
        return nil, fmt.Errorf("api key secret is required")
    }
    sum := sha256.Sum256([]byte(passphrase))
    return newCodecFromKey(sum[:])
}

func NewCodec(secret string) (*Codec, error) {
    key := []byte(secret)
    switch len(key) {
    case 16, 24, 32:
    default:
        return nil, fmt.Errorf("api key secret length must be 16, 24, or 32 bytes")
    }
    return newCodecFromKey(key)
}

func newCodecFromKey(key []byte) (*Codec, error) {
    block, err := aes.NewCipher(key)
    if err != nil { return nil, err }
    aead, err := cipher.NewGCM(block)
    if err != nil { return nil, err }
    return &Codec{aead: aead}, nil
}
```

**两种密钥派生方式：**

- `NewCodecFromPassphrase`：从任意长度字符串通过 SHA-256 哈希为 32 字节密钥。假设 `passphrase` 本身是高熵随机字符串（如环境变量中的 64 位 hex），而非用户输入的低熵密码。
- `NewCodec`：接受原始字节密钥，校验长度必须为 16/24/32 字节（对应 AES-128/192/256）。

**`NewCodec` 的长度校验**是防御性编程——如果传入的密钥长度不是 AES 支持的值，`aes.NewCipher` 也会报错，但显式检查提供了更清晰的错误信息。

### 2.2 加密流程 (`internal/pkg/secret/crypto.go:47-58`)

```go
func (c *Codec) Encrypt(plaintext string) (string, error) {
    nonce := make([]byte, c.aead.NonceSize())    // GCM default 12 bytes
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }

    sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)

    payload := make([]byte, 0, len(nonce)+len(sealed))
    payload = append(payload, nonce...)
    payload = append(payload, sealed...)
    return base64.StdEncoding.EncodeToString(payload), nil
}
```

**逐行分析：**

- **第 48-51 行 -- Nonce 生成：** `c.aead.NonceSize()` 对 GCM 返回 12。`rand.Reader` 是操作系统提供的 CSPRNG（密码学安全伪随机数生成器），在 Linux 上读取 `/dev/urandom`，在 Windows 上使用 `CryptGenRandom`。`io.ReadFull` 确保读满整个 nonce 缓冲区。
- **第 53 行 -- Seal 操作：** `c.aead.Seal(nil, nonce, []byte(plaintext), nil)` 的四个参数分别是：`dst`（输出缓冲区，`nil` 表示自动分配）、`nonce`、`plaintext`、`additionalData`（关联数据，此处为 `nil`）。`Seal` 返回的 `sealed` 包含加密后的密文和 16 字节的 GCM 认证标签。
- **第 55-57 行 -- 格式拼接：** 最终 payload 的格式是 `nonce(12B) || ciphertext(NB) || tag(16B)`。将 nonce 存储在密文前面是标准做法，因为 nonce 不需要保密，但每次加密必须唯一。

**安全特性：**
- 每次加密使用随机 nonce，保证相同明文加密出不同密文（语义安全）。
- GCM 认证标签确保密文未被篡改（完整性）。
- nonce + 认证标签的总开销为 28 字节，对于存储 API Key 等短文本来说可以忽略。

### 2.3 解密流程 (`internal/pkg/secret/crypto.go:60-78`)

```go
func (c *Codec) Decrypt(ciphertext string) (string, error) {
    payload, err := base64.StdEncoding.DecodeString(ciphertext)
    if err != nil { return "", err }

    nonceSize := c.aead.NonceSize()
    if len(payload) <= nonceSize {
        return "", fmt.Errorf("ciphertext is too short")
    }

    nonce := payload[:nonceSize]
    sealed := payload[nonceSize:]
    plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
    if err != nil { return "", err }
    return string(plaintext), nil
}
```

**逐行分析：**

- **第 61-63 行 -- Base64 解码：** 将存储的字符串还原为二进制。非法 Base64 字符会触发错误。
- **第 65-68 行 -- 长度校验：** `len(payload) <= nonceSize` 确保数据至少包含一个完整 nonce + 1 字节密文。注意 `<=` 而非 `<`，因为纯 nonce（无密文）也是无效的。
- **第 72-74 行 -- Open 操作：** `aead.Open` 同时完成解密和认证验证。如果 nonce、密文或认证标签任何一个被篡改，会返回错误。这是 GCM 模式的核心价值——不需要额外的 MAC 校验步骤。

### 2.4 API Key 脱敏 (`internal/pkg/secret/crypto.go:79-87`)

```go
func MaskAPIKey(key string) string {
    if key == "" {
        return ""
    }
    if len(key) <= 8 {
        return "****"
    }
    return key[:3] + "****" + key[len(key)-4:]
}
```

日志/展示用的工具函数。对于短密钥（<=8 字符），完全遮蔽；对于长密钥，保留首 3 尾 4 个字符。示例：`sk-abc123xyz` -> `sk-****xyz`。在调试时提供了足够的信息（可以识别是哪个 Key），又不会在日志中泄露完整密钥。

---

## 3. SSRF 防护

SSRF（Server-Side Request Forgery）是一种攻击手法，攻击者通过构造恶意 URL，诱使服务器向内部网络发起请求。VidLens 的 SSRF 防护采用了多层策略。

### 3.1 白名单配置 (`internal/service/remote_video_url.go:13-18`)

```go
var defaultAllowedVideoHosts = []string{
    "bilibili.com",
    "b23.tv",
    "youtube.com",
    "youtu.be",
}
```

默认白名单仅允许主流视频平台。可通过 `config.ToolsConfig.AllowedVideoHosts` 覆盖。

### 3.2 防护入口 (`internal/service/remote_video_url.go:52-96`)

```go
func (v remoteVideoURLValidator) validate(ctx context.Context, rawURL string) (checkedRemoteVideoURL, error) {
    rawURL = strings.TrimSpace(rawURL)
    parsed, err := neturl.Parse(rawURL)
    if err != nil {
        return checkedRemoteVideoURL{}, fmt.Errorf("invalid URL format")
    }

    // Layer 1: protocol whitelist
    scheme := strings.ToLower(parsed.Scheme)
    if scheme != "http" && scheme != "https" {
        return checkedRemoteVideoURL{}, fmt.Errorf("only http/https supported")
    }

    // Layer 2: host validation
    host := normalizeHost(parsed.Hostname())
    if host == "" {
        return checkedRemoteVideoURL{}, fmt.Errorf("URL missing host")
    }
    if host == "localhost" {
        return checkedRemoteVideoURL{}, fmt.Errorf("localhost not allowed")
    }
    if !hostAllowed(host, v.allowedHosts) {
        return checkedRemoteVideoURL{}, fmt.Errorf("unsupported domain: %s", host)
    }

    // Layer 3: IP safety check (SSRF core defense)
    if ip := net.ParseIP(host); ip != nil {
        if unsafeIP(ip) {
            return checkedRemoteVideoURL{}, fmt.Errorf("internal/local IP not allowed")
        }
    } else {
        ips, err := v.resolver.LookupIP(ctx, host)
        if err != nil {
            return checkedRemoteVideoURL{}, fmt.Errorf("DNS resolution failed: %w", err)
        }
        if len(ips) == 0 {
            return checkedRemoteVideoURL{}, fmt.Errorf("no DNS records")
        }
        for _, ip := range ips {
            if unsafeIP(ip) {
                return checkedRemoteVideoURL{}, fmt.Errorf("domain resolves to internal IP")
            }
        }
    }

    // Layer 4: URL sanitization
    sanitized := sanitizeRemoteVideoURL(*parsed)
    return checkedRemoteVideoURL{Raw: rawURL, Sanitized: sanitized, Host: host}, nil
}
```

**逐层分析：**

**第一层 -- 协议白名单（第 59-61 行）：**
`neturl.Parse` 是 Go 标准库的 URL 解析函数。只允许 `http` 和 `https` 协议，阻止了 `file://`、`gopher://`、`ftp://` 等可能被滥用的协议。

**第二层 -- 域名白名单（第 64-73 行）：**
`hostAllowed(host, v.allowedHosts)` 检查域名是否在允许列表中。这是防御 SSRF 的最有效手段——即使后续的 IP 检查被绕过，白名单也能阻止请求发往任意主机。

```go
func hostAllowed(host string, allowedHosts []string) bool {
    host = normalizeHost(host)
    for _, allowed := range allowedHosts {
        allowed = normalizeHost(allowed)
        if allowed == "" { continue }
        if host == allowed || strings.HasSuffix(host, "."+allowed) {
            return true  // bilibili.com or xxx.bilibili.com both pass
        }
    }
    return false
}
```

后缀匹配（`strings.HasSuffix(host, "."+allowed)`）允许子域名通过，如 `api.bilibili.com`。

**第三层 -- IP 安全检查（第 75-92 行）：**
代码区分了两种情况：
- **裸 IP：** 如果 URL 直接使用 IP 地址（如 `http://127.0.0.1/video`），`net.ParseIP` 返回非 nil，直接对 IP 调用 `unsafeIP` 检查。
- **域名：** 如果 URL 使用域名，需要先 DNS 解析得到 IP 列表，然后逐个检查。`v.resolver.LookupIP` 通过 `remoteURLResolver` 接口抽象，测试时可 mock。

```go
func unsafeIP(ip net.IP) bool {
    return ip == nil ||
        ip.IsLoopback() ||           // 127.0.0.0/8, ::1
        ip.IsPrivate() ||            // 10/8, 172.16/12, 192.168/16, fc00::/7
        ip.IsUnspecified() ||        // 0.0.0.0, ::
        ip.IsLinkLocalUnicast() ||   // 169.254/16, fe80::/10
        ip.IsLinkLocalMulticast() || // 224.0.0.0/24, ff02::/16
        ip.IsMulticast()             // 224.0.0.0/4, ff00::/8
}
```

覆盖了所有常见的内网和特殊地址段。`IsPrivate()` 在 Go 1.17+ 中引入，覆盖了 RFC 1918 和 RFC 4193 的私有地址范围。

**第四层 -- URL 清洗（第 94-95 行）：**

```go
func sanitizeRemoteVideoURL(parsed neturl.URL) string {
    parsed.User = nil    // strip credentials
    query := parsed.Query()
    parsed.RawQuery = "" // strip all query params
    parsed.Fragment = "" // strip fragment
    if isYouTubeWatchURL(parsed) {
        videoID := strings.TrimSpace(query.Get("v"))
        if videoID != "" {
            values := neturl.Values{}
            values.Set("v", videoID)
            parsed.RawQuery = values.Encode() // only keep v= for YouTube
        }
    }
    return parsed.String()
}
```

从原始 URL 中只保留 `Scheme`、`Host`、`Path`，去除 `User`（凭证）、`Query`、`Fragment` 等可能携带攻击载荷的部分。对 YouTube 做了特殊处理，保留 `v=` 参数（视频 ID）。这是一种白名单式的参数保留策略。

### 3.3 SSRF 防护的局限性

尽管代码采用了多层防护，仍存在以下理论上的攻击面：

1. **DNS Rebinding：** 攻击者注册一个域名，第一次 DNS 解析返回合法 IP（通过白名单检查），服务器发起 HTTP 请求时 DNS 再次解析返回内网 IP。代码在 DNS 解析和后续 HTTP 请求之间存在 TOCTOU 窗口。缓解方案：使用 DNS Pinning 或在 HTTP 客户端中限制连接的 IP。
2. **IPv6 映射地址：** `::ffff:127.0.0.1` 是 IPv4 映射的 IPv6 地址，`IsLoopback()` 能否正确识别取决于 Go 的实现。
3. **URL 格式怪异：** `http://evil.com#@trusted.com` 等利用 URL 解析差异的攻击向量。

---

## 4. CORS 配置 (`internal/middleware/cors.go`)

```go
func CORS() gin.HandlerFunc {
    return cors.New(cors.Config{
        AllowAllOrigins: true,
        AllowMethods:    []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowHeaders:    []string{"Origin", "Content-Type", "Authorization"},
        ExposeHeaders:   []string{"Content-Length"},
        MaxAge:          12 * time.Hour,
    })
}
```

**逐行分析：**

- **`AllowAllOrigins: true`：** 允许任意来源的跨域请求。这在开发阶段很方便，但在生产环境中应该限制为已知的前端域名列表。
- **`AllowMethods`：** 限制允许的 HTTP 方法为常见的 CRUD 操作。
- **`AllowHeaders`：** 允许的请求头，`Authorization` 是 JWT 认证所需的。
- **`MaxAge: 12 * time.Hour`：** 预检请求（Preflight）的缓存时间。浏览器在 12 小时内对同一来源的跨域请求不再发送 OPTIONS 预检，减少了网络开销。

**安全影响：** `AllowAllOrigins` 意味着任何网站都可以向 VidLens API 发起跨域请求。如果用户已登录（浏览器自动携带 Cookie），恶意网站可以读取用户的敏感数据。正确的做法是将 `AllowAllOrigins` 改为具体的域名白名单。

---

## 5. Redis 令牌桶限流 (`internal/middleware/ratelimit.go`)

### 5.1 限流器结构体 (`ratelimit.go:19-25`)

```go
type RateLimiter struct {
    client    redis.Cmdable
    capacity  int // global default bucket capacity
    rate      int // global default tokens per second
    overrides map[string]routeLimit
    mu        sync.RWMutex
}
```

支持全局默认配额 + 按路由覆盖（`SetRouteLimit`），实现"按用户和接口维度"限流。

### 5.2 Lua 令牌桶脚本 (`ratelimit.go:60-90`)

```go
var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local bucket = redis.call("HMGET", key, "tokens", "last_time")
local tokens = tonumber(bucket[1])
local last_time = tonumber(bucket[2])

if tokens == nil then
    tokens = capacity
    last_time = now
end

local elapsed = now - last_time
local new_tokens = elapsed / 1000 * rate
tokens = math.min(capacity, tokens + new_tokens)
last_time = now

if tokens >= 1 then
    tokens = tokens - 1
    redis.call("HMSET", key, "tokens", tokens, "last_time", last_time)
    redis.call("EXPIRE", key, 60)
    return 1
else
    redis.call("HMSET", key, "tokens", tokens, "last_time", last_time)
    redis.call("EXPIRE", key, 60)
    return 0
end
`)
```

**Lua 脚本分析：**

- **原子性：** Redis Lua 脚本在服务器端原子执行，不存在 GET-then-SET 的竞态。
- **令牌补充：** `elapsed / 1000 * rate` 按时间差补充令牌，`math.min(capacity, ...)` 防止超过桶容量。
- **令牌消费：** `tokens >= 1` 时消费一个令牌并返回 1（允许），否则返回 0（拒绝）。
- **自动过期：** `EXPIRE key 60` 保证空闲桶 60 秒后自动清理。

### 5.3 限流中间件 (`ratelimit.go:108-132`)

```go
func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
    return func(c *gin.Context) {
        path := c.FullPath()
        if path == "" {
            path = c.Request.URL.Path
        }

        var key string
        if userID, ok := c.Get("userID"); ok {
            key = fmt.Sprintf("%s:user:%v", path, userID)
        } else {
            key = fmt.Sprintf("%s:ip:%s", path, c.ClientIP())
        }

        capacity, rate := limiter.configFor(path)
        if !limiter.Allow(c.Request.Context(), key, capacity, rate) {
            response.TooManyRequests(c, "too many requests, please try again later")
            c.Abort()
            return
        }
        c.Next()
    }
}
```

**双维度计数：**
- 已登录用户：`{path}:user:{userID}` — 按用户隔离
- 未登录用户：`{path}:ip:{clientIP}` — 按 IP 隔离

**Fail-open 设计（`ratelimit.go:95-105`）：**

```go
func (r *RateLimiter) Allow(ctx context.Context, key string, capacity, rate int) bool {
    now := time.Now().UnixMilli()
    result, err := tokenBucketScript.Run(ctx, r.client,
        []string{fmt.Sprintf("rate_limiter:%s", key)},
        rate, capacity, now,
    ).Int()
    if err != nil {
        log.Printf("[ratelimit] Redis error, fail-open key=%s err=%v", key, err)
        return true
    }
    return result == 1
}
```

Redis 异常时 fail-open（放行）：限流是保护手段而非关键路径，不应成为单点故障。

---

## 总结：安全设计模式一览

| Module | Core Mechanism | Design Pattern | Potential Risk |
|--------|---------------|----------------|----------------|
| JWT Auth | HS256 signature + Gin middleware | Onion model (middleware intercept) | No token revocation, Context default value trap |
| AES-GCM | AES-256-GCM + random nonce | Authenticated Encryption (AEAD) | Key derivation strength depends on input entropy |
| SSRF Defense | Protocol whitelist + domain whitelist + IP check + URL sanitize | Defense-in-depth (multi-layer) | DNS Rebinding, TOCTOU |
| CORS | gin-contrib/cors | Middleware inject response headers | AllowAllOrigins risk in production |
| Rate Limit | Redis Lua token bucket + per-route override | Fail-open + dual-dimension counting | Redis outage disables rate limiting |
