package camera

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Discovery is read-only. Optional identity endpoints may be absent on OEM or
// older firmware. Encode describes settings; the product's stream limit excludes
// generic ExtraFormat placeholders that the camera cannot actually serve.
func (dahua) Discover(ctx context.Context, s Source) (Discovery, error) {
	c := s.Connection
	if c == nil {
		return Discovery{}, errors.New("invalid_connection")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, ResponseHeaderTimeout: 3 * time.Second, MaxResponseHeaderBytes: 64 << 10, IdleConnTimeout: 10 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	read := func(path string) (map[string]string, error) {
		raw, e := digestGet(ctx, client, *c, path)
		if e != nil {
			return nil, e
		}
		return parseValues(raw), nil
	}
	values, e := read("/cgi-bin/configManager.cgi?action=getConfig&name=Encode")
	if e != nil {
		return Discovery{}, e
	}
	profiles := parseProfiles(values)
	if len(profiles) == 0 {
		return Discovery{}, errors.New("camera_api_response_invalid")
	}
	maximum, e := dahuaMaxExtraStreams(read)
	if e != nil {
		return Discovery{}, e
	}
	available := profiles[:0]
	for _, profile := range profiles {
		if profile.ID != "main" {
			n, _ := strconv.Atoi(strings.TrimPrefix(profile.ID, "sub"))
			if n > maximum {
				continue
			}
		}
		available = append(available, profile)
	}
	profiles = available
	transportConfig, e := read("/cgi-bin/configManager.cgi?action=getConfig&name=RTSP")
	if e != nil {
		return Discovery{}, e
	}
	port, e := strconv.Atoi(transportConfig["RTSP.Port"])
	if e != nil || port < 1 || port > 65535 {
		return Discovery{}, errors.New("camera_api_response_invalid")
	}
	result := Discovery{Profiles: profiles, RTSPPort: port}
	// Missing optional information must not invalidate a usable Encode response.
	for _, item := range []struct {
		action string
		keys   []string
		dest   *string
	}{
		{"getDeviceType", []string{"type", "deviceType"}, &result.Device.Model},
		{"getSerialNo", []string{"sn", "serialNumber"}, &result.Device.Serial},
		{"getVendor", []string{"vendor"}, &result.Device.Vendor},
		{"getSoftwareVersion", []string{"version", "Version"}, &result.Device.Firmware},
	} {
		if ctx.Err() != nil {
			break
		}
		v, err := read("/cgi-bin/magicBox.cgi?action=" + item.action)
		if err != nil {
			continue
		}
		for _, k := range item.keys {
			if text := v[k]; text != "" && len(text) <= 256 && !strings.ContainsAny(text, "\r\n") {
				*item.dest = text
				break
			}
		}
	}
	return result, nil
}

// Firmware may expose this value through either endpoint. A missing or invalid
// limit is a failed discovery, so callers retain a known snapshot or stay pending.
func dahuaMaxExtraStreams(read func(string) (map[string]string, error)) (int, error) {
	var failure error
	for _, endpoint := range []struct{ path, key string }{
		{"/cgi-bin/magicBox.cgi?action=getProductDefinition&name=MaxExtraStream", "MaxExtraStream"},
		{"/cgi-bin/encode.cgi?action=getCaps", "caps.MaxExtraStream"},
	} {
		values, err := read(endpoint.path)
		if err != nil {
			if Code(err) == "camera_auth_failed" {
				return 0, err
			}
			failure = err
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(values[endpoint.key]))
		if err == nil && n >= 0 && n <= 99 {
			return n, nil
		}
		failure = errors.New("camera_api_response_invalid")
	}
	return 0, failure
}

func parseValues(raw []byte) map[string]string {
	result := map[string]string{}
	var decoded any
	if json.Unmarshal(raw, &decoded) == nil {
		var walk func(string, any)
		walk = func(key string, v any) {
			switch x := v.(type) {
			case map[string]any:
				for k, y := range x {
					next := k
					if key != "" {
						next = key + "." + k
					}
					walk(next, y)
				}
			case []any:
				for i, y := range x {
					walk(fmt.Sprintf("%s[%d]", key, i), y)
				}
			default:
				result[strings.TrimPrefix(key, "table.")] = fmt.Sprint(v)
			}
		}
		walk("", decoded)
		return result
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && len(key) < 256 && len(value) < 4096 {
			result[strings.TrimPrefix(strings.TrimSpace(key), "table.")] = strings.TrimSpace(value)
		}
	}
	return result
}

var encodeKey = regexp.MustCompile(`^Encode\[0\]\.(MainFormat|ExtraFormat)\[([0-9]+)\]\.(.+)$`)

func parseProfiles(values map[string]string) []Profile {
	grouped := map[int]map[string]string{}
	for k, v := range values {
		m := encodeKey.FindStringSubmatch(k)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		if m[1] == "MainFormat" {
			if n != 0 {
				continue
			}
		} else {
			n++
		}
		if n > 99 {
			continue
		}
		if grouped[n] == nil {
			grouped[n] = map[string]string{}
		}
		grouped[n][m[3]] = v
	}
	indices := []int{}
	for i := range grouped {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	out := []Profile{}
	for _, i := range indices {
		v := grouped[i]
		videoPresent := false
		for key := range v {
			if key == "VideoEnable" || strings.HasPrefix(key, "Video.") {
				videoPresent = true
				break
			}
		}
		if !videoPresent {
			continue
		}
		p := Profile{ID: "main", Name: "Principal"}
		if i > 0 {
			p.ID = "sub" + strconv.Itoa(i)
			p.Name = "Substream " + strconv.Itoa(i)
		}
		p.Codec = strings.ToLower(v["Video.Compression"])
		if len(p.Codec) > 32 {
			p.Codec = ""
		}
		p.Width, _ = strconv.Atoi(v["Video.Width"])
		p.Height, _ = strconv.Atoi(v["Video.Height"])
		p.FPS, _ = strconv.ParseFloat(v["Video.FPS"], 64)
		if raw, ok := v["VideoEnable"]; ok {
			if enabled, e := strconv.ParseBool(raw); e == nil {
				p.Enabled = &enabled
			}
		}
		if p.Width < 0 || p.Width > 32768 || p.Height < 0 || p.Height > 32768 || p.FPS < 0 || p.FPS > 1000 || math.IsNaN(p.FPS) || math.IsInf(p.FPS, 0) {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 || out[0].ID != "main" {
		return nil
	}
	if len(out) > 16 {
		out = out[:16]
	}
	return out
}

func digestGet(ctx context.Context, client *http.Client, c Connection, path string) ([]byte, error) {
	return digestRequest(ctx, client, c, "GET", path, nil)
}

func digestRequest(ctx context.Context, client *http.Client, c Connection, method, path string, payload []byte) ([]byte, error) {
	target := url.URL{Scheme: c.Protocol, Host: net.JoinHostPort(c.Host, strconv.Itoa(c.HTTPPort))}
	rel, _ := url.Parse(path)
	target.Path = rel.Path
	target.RawQuery = rel.RawQuery
	authorization := ""
	for attempt := 0; attempt < 3; attempt++ {
		req, e := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(payload))
		if e != nil {
			return nil, errors.New("invalid_connection")
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/xml")
		}
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		res, e := client.Do(req)
		if e != nil {
			if ctx.Err() != nil {
				return nil, errors.New("camera_api_timeout")
			}
			if n, ok := e.(net.Error); ok && n.Timeout() {
				return nil, errors.New("camera_api_timeout")
			}
			return nil, errors.New("camera_api_unavailable")
		}
		if res.StatusCode == http.StatusUnauthorized {
			challenge := res.Header.Get("WWW-Authenticate")
			io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
			res.Body.Close()
			if attempt == 2 {
				return nil, errors.New("camera_auth_failed")
			}
			authorization, e = digestAuthorizationMethod(challenge, c, method, req.URL.RequestURI())
			if e != nil {
				return nil, e
			}
			continue
		}
		if res.StatusCode == 403 {
			res.Body.Close()
			return nil, errors.New("camera_auth_failed")
		}
		if res.StatusCode < 200 || res.StatusCode > 299 {
			res.Body.Close()
			return nil, errors.New("camera_api_unsupported")
		}
		raw, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
		res.Body.Close()
		if e != nil || len(raw) > 1<<20 {
			return nil, errors.New("camera_api_response_invalid")
		}
		return raw, nil
	}
	return nil, errors.New("camera_auth_failed")
}

// Parse a quoted auth-param list without splitting commas inside realm/qop.
func digestParams(challenge string) (map[string]string, error) {
	if !strings.HasPrefix(strings.ToLower(challenge), "digest ") {
		return nil, errors.New("camera_auth_unsupported")
	}
	s := strings.TrimSpace(challenge[7:])
	out := map[string]string{}
	for s != "" {
		key, rest, ok := strings.Cut(s, "=")
		if !ok {
			return nil, errors.New("camera_auth_challenge_invalid")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		s = strings.TrimSpace(rest)
		value := ""
		if strings.HasPrefix(s, "\"") {
			s = s[1:]
			closed := false
			for len(s) > 0 {
				ch := s[0]
				s = s[1:]
				if ch == '"' {
					closed = true
					break
				}
				if ch == '\\' {
					if len(s) == 0 {
						return nil, errors.New("camera_auth_challenge_invalid")
					}
					ch = s[0]
					s = s[1:]
				}
				value += string(ch)
			}
			if !closed {
				return nil, errors.New("camera_auth_challenge_invalid")
			}
		} else {
			value, s, _ = strings.Cut(s, ",")
			value = strings.TrimSpace(value)
			out[key] = value
			s = strings.TrimSpace(s)
			continue
		}
		if key == "" {
			return nil, errors.New("camera_auth_challenge_invalid")
		}
		out[key] = value
		s = strings.TrimSpace(s)
		if s != "" {
			if s[0] != ',' {
				return nil, errors.New("camera_auth_challenge_invalid")
			}
			s = strings.TrimSpace(s[1:])
		}
	}
	if out["realm"] == "" || out["nonce"] == "" {
		return nil, errors.New("camera_auth_challenge_invalid")
	}
	return out, nil
}
func digestAuthorization(challenge string, c Connection, uri string) (string, error) {
	return digestAuthorizationMethod(challenge, c, "GET", uri)
}

func digestAuthorizationMethod(challenge string, c Connection, method, uri string) (string, error) {
	p, e := digestParams(challenge)
	if e != nil {
		return "", e
	}
	algorithm := p["algorithm"]
	if algorithm == "" {
		algorithm = "MD5"
	}
	base := strings.TrimSuffix(strings.ToUpper(algorithm), "-SESS")
	hash := func(s string) string { return "" }
	switch base {
	case "MD5":
		hash = func(s string) string { v := md5.Sum([]byte(s)); return hex.EncodeToString(v[:]) }
	case "SHA-256":
		hash = func(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
	case "SHA-512-256":
		hash = func(s string) string { v := sha512.Sum512_256([]byte(s)); return hex.EncodeToString(v[:]) }
	default:
		return "", errors.New("camera_auth_unsupported")
	}
	qop := ""
	if p["qop"] != "" {
		for _, v := range strings.Split(p["qop"], ",") {
			if strings.TrimSpace(v) == "auth" {
				qop = "auth"
			}
		}
		if qop == "" {
			return "", errors.New("camera_auth_unsupported")
		}
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return "", errors.New("camera_auth_unavailable")
	}
	cnonce := hex.EncodeToString(nonce)
	pass := ""
	if c.Password != nil {
		pass = *c.Password
	}
	ha1 := hash(c.Username + ":" + p["realm"] + ":" + pass)
	if strings.HasSuffix(strings.ToUpper(algorithm), "-SESS") {
		ha1 = hash(ha1 + ":" + p["nonce"] + ":" + cnonce)
	}
	ha2 := hash(method + ":" + uri)
	response := hash(ha1 + ":" + p["nonce"] + ":" + ha2)
	if qop != "" {
		response = hash(ha1 + ":" + p["nonce"] + ":00000001:" + cnonce + ":" + qop + ":" + ha2)
	}
	quote := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	fields := []string{"username=" + quote(c.Username), "realm=" + quote(p["realm"]), "nonce=" + quote(p["nonce"]), "uri=" + quote(uri), "response=" + quote(response), "algorithm=" + algorithm}
	if p["opaque"] != "" {
		fields = append(fields, "opaque="+quote(p["opaque"]))
	}
	if qop != "" {
		fields = append(fields, "qop=auth", "nc=00000001", "cnonce="+quote(cnonce))
	} else if strings.HasSuffix(strings.ToUpper(algorithm), "-SESS") {
		fields = append(fields, "cnonce="+quote(cnonce))
	}
	return "Digest " + strings.Join(fields, ", "), nil
}
