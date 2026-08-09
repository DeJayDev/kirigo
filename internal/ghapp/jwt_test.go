package ghapp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	block, _ := pem.Decode([]byte(testPrivateKeyPEM))
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}
	return key
}

const testPrivateKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAvPOmfLr2bwUspY9p3hN87G5MfLVFQZlUG91PnHGvOlvU1+FG
se0H32QhjwRZI6nZeqm8kxWn8QzO/aDh6RBcdlafwGCqSpoITHVCUUIfJFlD/nLW
V+AdN+xGBOmJLjSEiSKXAaFX2ugKa6+EeFvvb4bpg8AvD24qxCfHTyGGwEzbP9WH
xtVW7FK8sBt2WtdyVgxxWQJzyptI3A7hrFFIcBPOPx5kABsA6hPC/dhHlflSWtMC
AEj2NOgpEIbs18Ed4j7WTqdgO+VyLbNTs1FeMXtPSmdxT4xBnruvapxXZW7F+LP1
ylWNM3GIaJA3D3PXZbn6n7NqT4IyUoWjgga99QIDAQABAoIBABds6Z4bG8RF0vQv
pINozRsOzaLEYnKwjLHrrRwLKBtHGUsuXbJfXZN/eX2en2KBgznm4z8k9y42VU8y
a++WHLB7KtER6urWz+KSwwcJ+IxsGLbqC9LXMSgmvcOMJHq6/hd2V1xXYWn9TJVv
LDAzzap5AmhYIj16fgjVmasgz/D1H3+V2fRl3F5BwCBAY+HYYn9ARBVekDR5m2Qd
l+l6CLikhtN8lj/PYQaQ1pTQKKa44vuBBzv9PeIU7Ok+GYjdWqp48HwSbruKkUed
67hUGcVcVItGe0azLjL/x/neXoaCIwilTai5xdlSj50MigRbRrzdYUDdBIb60Rpa
jeDAnF0CgYEA9E1u6JhJcFaqqlgWwsHUEKEzlXWGFVQJ7jH/x5sesjtMR6OK4UHg
gLiIVdYNfh8JUvvli3nGQtvir57u/4aCZ9z6orvlZnfo6obccC2CDwCsJPSylLbo
JAPM2SOmoppgvgVh8E40+xydZcFQuyldFFK0zfqeVZdPT6bUekV6JXcCgYEAxf+/
xYTSA0C9U8kFK4Ixd3VNtd7NY4DQT40CZ0JBvFc1oWOK4swqqFYI6rl97NHqLYx8
xKn5JFsLPZLlkamgptbeNmCJfQ8XYU0RUy2YCq2ryQUb5+7tH0T7CAHiUQsvAKXE
866e1dv1mTqi227y919kIoXxt40l/wWwVacXwvMCgYA+ySPG3VSKbYuhCdCXrw7c
U0GZmMGj+5wtvmXZG9GwxrKc+rf3mmGjU0mencuL7VgNHrXouZwtlKtWrUcJHr2n
CdDUP+v+ALU4iP5gSiHRzz9upAC9XaCdmOhtqc7qnThdva5k/wR4wOrmut8PqtrE
HVlgUpecsa1tcBfNcMuqkwKBgG13ZQYF1bpYq5PL+qDAXSrnXqxjXvhZOlIQ6rg+
CGvhZ1Qv3ZRgPmFNF6b2IKmysJ64Ii70rjqsXz2Onn924cv71WUI4FqU4l84JZDw
DzQwKl58BZ6oGM8F6yfVKtOVtEnOXGJBM62W62To5yscXxXm1kzD8wyA6/Xfpkrk
k52DAoGBAKi9nG82w8QzQ7/1TEagIIkKIIvgTpqbo6Ra5JPZpwe76VYpWQ0x4pSU
ndUCV7zxeVnFJCMkq0bsNXbs73EyXH6YuoLuqQWCHALezFUILCeqsR03w+ZrV98w
ZMsHf8pbUyG1VJdE6JxVWflDRP0ivG8e8IUPSd1z/WYayLCH0ACT
-----END RSA PRIVATE KEY-----`

func TestSignAppJWTShapeAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	token, err := SignAppJWT("123456", testKey(t), now)
	if err != nil {
		t.Fatalf("SignAppJWT: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments, want 3", len(parts))
	}

	var claims struct {
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
		Iss string `json:"iss"`
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != "123456" {
		t.Fatalf("iss = %q, want 123456", claims.Iss)
	}
	if claims.Iat != now.Add(-30*time.Second).Unix() {
		t.Fatalf("iat = %d, want backdated", claims.Iat)
	}
	if ttl := claims.Exp - now.Unix(); ttl > 600 || ttl <= 0 {
		t.Fatalf("exp ttl = %ds, want within GitHub's 10 min limit", ttl)
	}
}

func TestSignAppJWTRequiresKeyAndID(t *testing.T) {
	if _, err := SignAppJWT("", testKey(t), time.Now()); err == nil {
		t.Fatal("expected error for empty app id")
	}
	if _, err := SignAppJWT("1", nil, time.Now()); err == nil {
		t.Fatal("expected error for nil key")
	}
}

func TestParsePrivateKeyRejectsGarbage(t *testing.T) {
	if _, err := ParsePrivateKey([]byte("not a pem")); err == nil {
		t.Fatal("expected error for non-PEM input")
	}
}
