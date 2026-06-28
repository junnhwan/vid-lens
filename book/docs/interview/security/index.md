# VidLens 安全模块 -- 面试题集

## 题目 1: JWT 中间件的 Bearer 提取逻辑

**源码位置:** `internal/middleware/auth.go:12-40`

```go
 12 func JWTAuth(secret string) gin.HandlerFunc {
 13     return func(c *gin.Context) {
 14         authHeader := c.GetHeader("Authorization")
 15         if authHeader == "" {
 16             response.Unauthorized(c, "请先登录")
 17             c.Abort()
 18             return
 19         }
 20
 21         parts := strings.SplitN(authHeader, " ", 2)
 22         if len(parts) != 2 || parts[0] != "Bearer" {
 23             response.Unauthorized(c, "Token 格式错误")
 24             c.Abort()
 25             return
 26         }
 27
 28         claims, err := jwt.ParseToken(parts[1], secret)
 29         if err != nil {
 30             response.Unauthorized(c, "Token 无效或已过期")
 31             c.Abort()
 32             return
 33         }
 34
 35         c.Set("userID", claims.UserID)
 36         c.Set("username", claims.Username)
 37         c.Set("role", claims.Role)
 38         c.Next()
 39     }
 40 }
```

**问题:** 代码使用 `strings.SplitN(authHeader, " ", 2)` 将 `Bearer <token>` 拆为两部分。为什么选择 SplitN 而不是 HasPrefix + TrimPrefix?

**追问链:**
1. 如果攻击者发送 `Authorization: BearerXXX`(无空格), 这段代码会怎么处理? 这是否符合预期?
2. `strings.TrimPrefix` 只去除**第一个**匹配。如果 token 本身以 `"Bearer "` 开头(极端情况), 会不会造成解析偏差?
3. 为什么不直接用 `strings.CutPrefix`(Go 1.20+) 或正则来提取 token? 各自的优劣是什么?
4. 如果在 `TrimPrefix` 之后再做一次 `strings.TrimSpace`, 是否有必要?

---

## 题目 2: Claims 从 Context 到 Handler 的传递方式

**源码位置:** `internal/middleware/auth.go:35-38` 及 `auth.go:42-49`

```go
 35         c.Set("userID", claims.UserID)
 36         c.Set("username", claims.Username)
 37         c.Set("role", claims.Role)
 38         c.Next()
 39     }
 40 }
 41
 42 func GetUserID(c *gin.Context) int64 {
 43     id, exists := c.Get("userID")
 44     if !exists {
 45         return 0
 46     }
 47     return id.(int64)
 48 }
```

**问题:** `GetUserID` 在 key 不存在时返回 `0`。这在业务层会产生什么后果?

**追问链:**
1. 如果某个路由忘记挂载 `JWTAuth` 中间件, `GetUserID` 会静默返回 `0`。在什么场景下这会导致越权?
2. `v.(int64)` 是硬类型断言。如果未来有人在 Context 中存入了 `float64` 类型的 userID(例如 JSON 反序列化), 会发生什么?
3. 更安全的写法应该怎么做? 是否应该返回 `(int64, error)` 或 panic?
4. Gin 的 `c.Set` 底层是一个 `map[string]any`, 它是否线程安全? 在并发 handler 中使用是否安全?

---

## 题目 3: HS256 签名与密钥管理

**源码位置:** `internal/pkg/jwt/jwt.go:19-34`

```go
 19 func GenerateToken(userID int64, username, role, secret string, expireHours int) (string, error) {
 20     now := time.Now()
 21     claims := Claims{
 22         UserID:   userID,
 23         Username: username,
 24         Role:     role,
 25         RegisteredClaims: jwt.RegisteredClaims{
 26             ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireHours) * time.Hour)),
 27             IssuedAt:  jwt.NewNumericDate(now),
 28             Issuer:    "vidlens",
 29         },
 30     }
 31
 32     token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
 33     return token.SignedString([]byte(secret))
 34 }
```

**问题:** 代码使用 HS256(对称签名)。从安全工程角度, 这带来了哪些局限?

**追问链:**
1. HS256 要求签发方和验证方共享同一个 `secret`。如果微服务 A 和微服务 B 都需要验证 token, 密钥如何安全分发?
2. 对比 RS256(非对称签名), 各自的适用场景是什么? VidLens 这种单体应用用 HS256 是否合理?
3. `expireHours` 是调用方传入的整数。如果传入 `0` 或负数, token 的行为是什么?
4. 第 26 行设置了 `IssuedAt`, 但代码没有做时钟偏移(clock skew)容错。在分布式场景下会出什么问题?

---

## 题目 4: AES-GCM 密钥派生

**源码位置:** `internal/pkg/secret/crypto.go:17-23`

```go
 17 func NewCodecFromPassphrase(passphrase string) (*Codec, error) {
 18     if passphrase == "" {
 19         return nil, fmt.Errorf("api key secret is required")
 20     }
 21     sum := sha256.Sum256([]byte(passphrase))
 22     return newCodecFromKey(sum[:])
 23 }
```

**问题:** 第 21 行用 `sha256.Sum256` 将字符串转为 32 字节密钥。这与专业的密钥派生函数(如 PBKDF2、Argon2)相比, 缺少了什么?

**追问链:**
1. `sha256.Sum256` 是确定性的、无盐的。如果两个用户碰巧使用了相同的 `secret` 字符串, 他们的密钥就完全相同。这在什么场景下是可接受的, 什么场景下不可接受?
2. 如果 `secret` 来自用户输入的密码, 直接 SHA256 有什么风险? 暴力破解的速度大约是多少(提示: SHA256 的 GPU 算力)?
3. PBKDF2 和 Argon2 的核心区别是什么? 为什么 Argon2 更适合密码场景?
4. 这里 `secret` 如果是环境变量或配置文件中的随机字符串(而非用户密码), SHA256 派生是否足够?

---

## 题目 5: AES-GCM Nonce 生成与密文格式

**源码位置:** `internal/pkg/secret/crypto.go:47-58`

```go
 47 func (c *Codec) Encrypt(plaintext string) (string, error) {
 48     nonce := make([]byte, c.aead.NonceSize())
 49     if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
 50         return "", err
 51     }
 52
 53     sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
 54     payload := make([]byte, 0, len(nonce)+len(sealed))
 55     payload = append(payload, nonce...)
 56     payload = append(payload, sealed...)
 57     return base64.StdEncoding.EncodeToString(payload), nil
 58 }
```

**问题:** 第 17 行使用 `rand.Reader` 生成 nonce。GCM 的 nonce 大小通常是 12 字节。如果用生日攻击估算, 在同一个密钥下大约加密多少次就有 50% 的概率发生 nonce 碰撞?

**追问链:**
1. GCM nonce 碰撞的后果是什么? (提示: 与 XOR 和认证标签有关)
2. 代码使用 `rand.Reader`(CSPRNG), 而非 `math/rand`。两者的核心区别是什么? 如果误用 `math/rand`, 会带来什么风险?
3. 密文格式是 `nonce || ciphertext || tag`(第 22-24 行拼接)。如果攻击者可以篡改密文中的 nonce 部分, `Decrypt` 能否检测到?
4. 如果要支持关联数据(AAD), 代码需要怎么改? 什么场景需要 AAD?

---

## 题目 6: AES-GCM 解密的长度校验

**源码位置:** `internal/pkg/secret/crypto.go:60-77`

```go
 60 func (c *Codec) Decrypt(ciphertext string) (string, error) {
 61     payload, err := base64.StdEncoding.DecodeString(ciphertext)
 62     if err != nil {
 63         return "", err
 64     }
 65     nonceSize := c.aead.NonceSize()
 66     if len(payload) <= nonceSize {
 67         return "", fmt.Errorf("ciphertext is too short")
 68     }
 69
 70     nonce := payload[:nonceSize]
 71     sealed := payload[nonceSize:]
 72     plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
 73     if err != nil {
 74         return "", err
 75     }
 76     return string(plaintext), nil
 77 }
```

**问题:** 第 32 行检查 `len(data) < nonceSize`, 但没有检查 `sealed` 部分的最小长度。GCM 密文的最短合法长度是多少?

**追问链:**
1. GCM 的 `Seal` 输出包含密文和 16 字节的认证标签。那么 `sealed` 部分的最短合法长度是多少?
2. 如果传入 `len(data) == nonceSize + 1`(即 sealed 只有 1 字节), `aead.Open` 会返回什么错误? 这个错误信息是否足够安全(不泄露内部细节)?
3. 代码在第 32 行的错误消息是 `"密文过短"`。从安全角度, 是否应该返回更模糊的错误消息(如 `"解密失败"`)以避免信息泄露?
4. `base64.StdEncoding.DecodeString` 是否会因为非法字符而 panic? 如果会, 如何防御?

---

## 题目 7: API Key 脱敏函数

**源码位置:** `internal/pkg/secret/crypto.go:79-87`

```go
 79 func MaskAPIKey(key string) string {
 80     if key == "" {
 81         return ""
 82     }
 83     if len(key) <= 8 {
 84         return "****"
 85     }
 86     return key[:3] + "****" + key[len(key)-4:]
 87 }
```

**问题:** 这个函数对短密钥(<=8 字符)的处理方式是返回固定字符串 `"****"`。但对于刚好 9 字符的密钥, 泄露了首尾各 4 个字符, 仅隐藏 1 个字符。这在安全上是否可接受?

**追问链:**
1. 如果 API Key 的格式是 `sk-` 前缀(如 OpenAI 的 `sk-abc...xyz`), 首 4 个字符 `sk-a` 已经泄露了协议前缀。这是否降低了安全性?
2. 对比 AWS 的做法(只显示最后 4 个字符), 哪种更安全? 为什么?
3. 如果攻击者同时获取了多个脱敏后的 API Key(如 `sk-a****xyz` 和 `sk-a****uvw`), 能否缩小暴力搜索空间?
4. 更好的脱敏策略应该是什么? 是否应该根据 Key 的总长度动态调整可见字符数?

---

## 题目 8: SSRF 防护 -- 域名白名单与 IP 检查

**源码位置:** `internal/service/remote_video_url.go:52-96`

```go
 52 func (v remoteVideoURLValidator) validate(ctx context.Context, rawURL string) (checkedRemoteVideoURL, error) {
 53     rawURL = strings.TrimSpace(rawURL)
 54     parsed, err := neturl.Parse(rawURL)
 55     if err != nil {
 56         return checkedRemoteVideoURL{}, fmt.Errorf("视频链接格式错误")
 57     }
 58
 59     scheme := strings.ToLower(parsed.Scheme)
 60     if scheme != "http" && scheme != "https" {
 61         return checkedRemoteVideoURL{}, fmt.Errorf("仅支持 http/https 视频链接")
 62     }
 63
 64     host := normalizeHost(parsed.Hostname())
 65     if host == "" {
 66         return checkedRemoteVideoURL{}, fmt.Errorf("视频链接缺少 host")
 67     }
 68     if host == "localhost" {
 69         return checkedRemoteVideoURL{}, fmt.Errorf("不允许访问本地地址")
 70     }
 71     if !hostAllowed(host, v.allowedHosts) {
 72         return checkedRemoteVideoURL{}, fmt.Errorf("不支持的视频平台域名: %s", host)
 73     }
 74
 75     if ip := net.ParseIP(host); ip != nil {
 76         if unsafeIP(ip) {
 77             return checkedRemoteVideoURL{}, fmt.Errorf("不允许访问内网或本地地址")
 78         }
 79     } else {
 80         ips, err := v.resolver.LookupIP(ctx, host)
 81         if err != nil {
 82             return checkedRemoteVideoURL{}, fmt.Errorf("解析视频链接域名失败: %w", err)
 83         }
 84         if len(ips) == 0 {
 85             return checkedRemoteVideoURL{}, fmt.Errorf("视频链接域名没有可用解析结果")
 86         }
 87         for _, ip := range ips {
 88             if unsafeIP(ip) {
 89                 return checkedRemoteVideoURL{}, fmt.Errorf("视频链接域名解析到内网或本地地址")
 90             }
 91         }
 92     }
 93
 94     sanitized := sanitizeRemoteVideoURL(*parsed)
 95     return checkedRemoteVideoURL{Raw: rawURL, Sanitized: sanitized, Host: host}, nil
 96 }
```

**问题:** 第 61 行对裸 IP 和域名分别处理。但如果攻击者使用 `0x7f000001`(十六进制 IP) 或 `2130706433`(十进制 IP) 来表示 `127.0.0.1`, `net.ParseIP` 能否正确解析?

**追问链:**
1. `net.ParseIP("0x7f000001")` 返回什么? 这是否绕过了 `unsafeIP` 检查?
2. 更广泛的 SSRF 绕过手法有哪些? (提示: DNS Rebinding、IPv6 映射地址如 `::ffff:127.0.0.1`、URL 中的 `@` 用户信息)
3. 第 53 行的 `url.Parse` 是否会处理 `http://evil.com@trusted.com` 这种 userinfo 攻击? 最终 `Hostname()` 返回的是什么?
4. DNS Rebinding 攻击是如何工作的? 为什么第 66 行的 DNS 解析和后续的 HTTP 请求之间存在 TOCTOU(Time-of-Check-Time-of-Use) 窗口?

---

## 题目 9: SSRF 防护 -- URL 清洗与特殊处理

**源码位置:** `internal/service/remote_video_url.go:126-140`

```go
126 func sanitizeRemoteVideoURL(parsed neturl.URL) string {
127     parsed.User = nil
128     query := parsed.Query()
129     parsed.RawQuery = ""
130     parsed.Fragment = ""
131     if isYouTubeWatchURL(parsed) {
132         videoID := strings.TrimSpace(query.Get("v"))
133         if videoID != "" {
134             values := neturl.Values{}
135             values.Set("v", videoID)
136             parsed.RawQuery = values.Encode()
137         }
138     }
139     return parsed.String()
140 }
```

**问题:** 第 76 行只保留了 `v=` 参数, 但 `parsed.Query().Get("v")` 的值未经任何校验。如果攻击者传入 `v=<script>alert(1)</script>`, 这段代码的行为是什么?

**追问链:**
1. `v=<script>alert(1)</script>` 经过 `url.URL.String()` 编码后会变成什么? 是否仍然存在 XSS 风险?
2. 如果 `sanitized.String()` 最终被嵌入到 HTML 页面中(例如作为 `<iframe src="...">` 的属性), 是否需要额外的 HTML 转义?
3. 除了 `v` 参数, YouTube URL 还有哪些可被滥用的参数(如 `list`、`t`、`start`)? 是否应该用白名单而非黑名单来过滤参数?
4. 如果未来需要支持更多视频平台(Bilibili、抖音等), 这种逐平台特殊处理的方式是否可维护? 更好的架构是什么?

---

## 题目 10: CORS 配置与测试中间件绕过

**源码位置:** `internal/middleware/cors.go:11-19`

```go
// middleware/cors.go
func CORS() gin.HandlerFunc {
    return cors.New(cors.Config{
        AllowAllOrigins: true,
        AllowMethods:    []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowHeaders:    []string{"Origin", "Content-Type", "Authorization"},
        ExposeHeaders:   []string{"Content-Length"},
        MaxAge:          12 * time.Hour,
    })
}

// ai_profile_test.go:43-46
router.POST("/ai/profiles/test", func(c *gin.Context) {
    c.Set("userID", int64(7))
    handler.Test(c)
})
```

**问题:** CORS 配置中 `AllowAllOrigins: true` 但未设置 `AllowCredentials`。这种配置在生产环境中有什么安全风险?

**追问链:**
1. `AllowAllOrigins: true` 意味着任何域名都可以向 VidLens API 发起跨域请求。如果用户已登录（携带 Cookie），恶意网站可以读取用户的敏感数据。正确的做法是什么?
2. 对比 `AllowAllOrigins` 和具体的域名白名单（如 `AllowOrigins: []string{"https://vidlens.example.com"}`），各自适用什么场景?
3. 测试代码（`c.Set("userID", int64(7))`）直接在路由中注入用户身份，绕过了 JWT 中间件。这种测试模式有什么风险? 如果测试代码意外泄漏到生产环境会怎样?
4. 正确的测试方式应该如何处理认证? (提示: mock middleware、test token、httptest)
