package passthrough

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// IsPassthroughPath reports whether path carries an absolute upstream URL,
// in either the "/https://host/..." form the caller sends or the
// "/https:/host/..." form left behind by a middlebox that collapses
// duplicate slashes.
func IsPassthroughPath(path string) bool {
	body := strings.TrimPrefix(path, "/")
	scheme, rest, ok := strings.Cut(body, ":")
	if !ok {
		return false
	}
	return (scheme == "http" || scheme == "https") && strings.TrimLeft(rest, "/") != ""
}

// ParseTarget rebuilds the absolute upstream URL embedded in path.
func ParseTarget(path string) (*url.URL, error) {
	body := strings.TrimPrefix(path, "/")
	scheme, rest, ok := strings.Cut(body, ":")
	if !ok || (scheme != "http" && scheme != "https") {
		return nil, fmt.Errorf("path must start with http:// or https://")
	}
	target := strings.TrimLeft(rest, "/")
	if target == "" {
		return nil, fmt.Errorf("missing upstream host in %q", path)
	}

	u, err := url.Parse(scheme + "://" + target)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream URL: %v", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing upstream host in %q", path)
	}
	if u.User != nil {
		return nil, fmt.Errorf("credentials in the upstream URL are not allowed")
	}
	return u, nil
}

// mergeQuery folds the client's raw query string into the upstream URL without
// double-encoding what the path already carried.
func mergeQuery(targetQuery, rawQuery string) string {
	switch {
	case targetQuery == "":
		return rawQuery
	case rawQuery == "":
		return targetQuery
	default:
		return targetQuery + "&" + rawQuery
	}
}

// BlockedIP reports whether ip must not be dialed: loopback, private,
// link-local (covers cloud metadata at 169.254.169.254), multicast or CGNAT.
func BlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 (CGNAT) and the 0.0.0.0/8 "this network" range.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
		if v4[0] == 0 {
			return true
		}
	}
	return false
}
