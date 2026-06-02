package utils

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	maskURLPattern    = regexp.MustCompile(`https?://[^\s/$.?#].[^\s]*`)
	maskDomainPattern = regexp.MustCompile(`\b(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}\b`)
	maskIPPattern     = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// 同时容忍:
	//   bare 形态  api_key:VALUE
	//   JSON 标准 "api_key": "VALUE"  (key/value 可独立带或不带引号、冒号前后允许空白)
	// value 字符集排除空白/引号/JSON 容器收尾字符(} , ]),避免裸形态把尾部标点一起吞掉。
	maskApiKeyPattern = regexp.MustCompile(`(['"]?)api_key(['"]?)(\s*:\s*)(['"]?)([^\s'"},\]]+)(['"]?)`)
)

// cctldSecondLevelLabels 列出 ".co.uk / .com.cn / .ne.jp" 这类 cctld 下常见的次级域标签。
// 仅当主机名末两段是 "<这些标签>.<2-letter tld>" 时,才把末两段整体视作 TLD 保留;
// 否则 api.io / app.ai / name.gg 这种短主机名 + 2 字母 TLD 会被误当 cctld 形态,前一段被漏脱。
var cctldSecondLevelLabels = map[string]struct{}{
	"co":  {},
	"com": {},
	"net": {},
	"org": {},
	"gov": {},
	"edu": {},
	"ac":  {},
	"or":  {},
	"ne":  {},
	"go":  {},
}

func maskHostTail(parts []string) []string {
	if len(parts) < 2 {
		return parts
	}
	lastPart := parts[len(parts)-1]
	secondLastPart := parts[len(parts)-2]
	if len(lastPart) == 2 {
		if _, ok := cctldSecondLevelLabels[secondLastPart]; ok {
			return []string{secondLastPart, lastPart}
		}
	}
	return []string{lastPart}
}

func maskHostForURL(host string) string {
	// host 为 IPv4(可能带端口) 时直接整段替换,避免末位泄露(如 1.2.3.4 -> ***.4)。
	if maskIPPattern.MatchString(host) {
		return maskIPPattern.ReplaceAllString(host, "***.***.***.***")
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "***"
	}
	tail := maskHostTail(parts)
	return "***." + strings.Join(tail, ".")
}

func maskHostForPlainDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return domain
	}
	tail := maskHostTail(parts)
	numStars := len(parts) - len(tail)
	if numStars < 1 {
		numStars = 1
	}
	stars := strings.TrimSuffix(strings.Repeat("***.", numStars), ".")
	return stars + "." + strings.Join(tail, ".")
}

// MaskSensitiveInfo 屏蔽错误消息中的 URL、域名、IP 与 api_key 字面量,
// 用于将上游错误回写客户端前的最后一道脱敏。规则与 new-api 对齐。
func MaskSensitiveInfo(str string) string {
	str = maskURLPattern.ReplaceAllStringFunc(str, func(urlStr string) string {
		u, err := url.Parse(urlStr)
		if err != nil {
			return urlStr
		}

		host := u.Host
		if host == "" {
			return urlStr
		}

		maskedHost := maskHostForURL(host)
		result := u.Scheme + "://" + maskedHost

		if u.Path != "" && u.Path != "/" {
			pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
			maskedPathParts := make([]string, len(pathParts))
			for i := range pathParts {
				if pathParts[i] != "" {
					maskedPathParts[i] = "***"
				}
			}
			if len(maskedPathParts) > 0 {
				result += "/" + strings.Join(maskedPathParts, "/")
			}
		} else if u.Path == "/" {
			result += "/"
		}

		if u.RawQuery != "" {
			values, err := url.ParseQuery(u.RawQuery)
			if err != nil {
				result += "?***"
			} else {
				maskedParams := make([]string, 0, len(values))
				for key := range values {
					maskedParams = append(maskedParams, key+"=***")
				}
				if len(maskedParams) > 0 {
					result += "?" + strings.Join(maskedParams, "&")
				}
			}
		}

		return result
	})

	str = maskDomainPattern.ReplaceAllStringFunc(str, func(domain string) string {
		return maskHostForPlainDomain(domain)
	})

	str = maskIPPattern.ReplaceAllString(str, "***.***.***.***")
	str = maskApiKeyPattern.ReplaceAllString(str, "${1}api_key${2}${3}${4}***${6}")

	return str
}
