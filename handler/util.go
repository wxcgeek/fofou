package handler

import (
	"bytes"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/fofou/common"
)

var rxBot = regexp.MustCompile(`(bot|crawl|spider)`)

func intmin(a, b int) int {
	if a < b {
		return a
	} else {
		return b
	}
}

func intdivceil(a, b int) int {
	return int(math.Ceil(float64(a) / float64(b)))
}

func atoi(v string) (byte, int8, uint16, int16, uint32, int32, uint64, int64, uint, int) {
	i, _ := strconv.ParseUint(v, 10, 64)
	return byte(i), int8(i), uint16(i), int16(i), uint32(i), int32(i), i, int64(i), uint(i), int(i)
}

func getIPAddress(r *http.Request) (v [8]byte) {
	ipAddr := ""
	hdrRealIP, hdrForwardedFor := r.Header.Get("X-Real-Ip"), r.Header.Get("X-Forwarded-For")

	if hdrRealIP == "" && hdrForwardedFor == "" {
		s := r.RemoteAddr
		idx := strings.LastIndex(s, ":")
		if idx == -1 {
			ipAddr = s
		} else {
			ipAddr = s[:idx]
		}
	} else if hdrForwardedFor != "" {
		parts := strings.Split(hdrForwardedFor, ",")
		ipAddr = strings.TrimSpace(parts[0])
	} else {
		ipAddr = hdrRealIP
	}

	ip := net.ParseIP(ipAddr)
	if len(ip) == 0 {
		return
	}
	ipv4 := ip.To4()
	if len(ipv4) == 0 {
		copy(v[:], ip)
		return
	}
	copy(v[4:], ipv4[:3])
	return
}

func throtNewPost(ip, id [8]byte) bool {
	if id != [8]byte{} {
		// use ID whenever possible
		ip = id
	}

	now := time.Now().Unix()
	ts, ok := common.KthrotIPID.Get(ip)
	if !ok {
		common.KthrotIPID.Add(ip, now)
		return true
	}
	t := ts.(int64)
	if now-t > int64(common.Kforum.Cooldown) {
		common.KthrotIPID.Add(ip, now)
		return true
	}
	return false
}

func writeSimpleJSON(w http.ResponseWriter, args ...interface{}) {
	var p bytes.Buffer
	p.WriteString("{")
	for i := 0; i < len(args); i += 2 {
		k, _ := args[i].(string)
		p.WriteByte('"')
		p.WriteString(k)
		p.WriteString(`":`)
		buf, _ := json.Marshal(args[i+1])
		p.Write(buf)
		p.WriteByte(',')
	}
	if len(args) > 0 {
		p.Truncate(p.Len() - 1)
	}
	p.WriteString("}")
	w.Write(p.Bytes())
}

func sanitizeFilename(name string) string {
	const needle = "\\/:*?\"<>| "
	if !strings.ContainsAny(name, needle) && len(name) <= 32 {
		return name
	}
	buf := []rune(name)
	for i := 0; i < len(buf); i++ {
		if i >= 32 {
			buf = buf[:i]
			break
		}
		if strings.ContainsRune(needle, buf[i]) {
			buf[i] = '_'
		}
	}
	return string(buf)
}

var reMessage = regexp.MustCompile("(`{3,})")
