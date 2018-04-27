// This code is under BSD license. See license-bsd.txt
package main

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

func isSp(c rune) bool {
	return c == ' '
}

func isNewline(s string) bool {
	return 1 == len(s) && s[0] == '\n'
}

func isNewlineChar(c rune) bool {
	return c == '\n'
}

func endsSendence(s string) bool {
	n := len(s)
	if 0 == n {
		return false
	}
	c := s[n-1]
	if c == '.' || c == '?' || c == '\n' {
		return true
	}
	return false
}

// TODO: this is a bit clumsy. Would be much faster (and probably cleaner) to
// go over string char-by-char
// TODO: only do it if detects high CAPS rate
func UnCaps(s string) string {
	parts := strings.FieldsFunc(s, isSp)
	n := len(parts)
	res := make([]string, n, n)
	sentenceStart := true
	for i := 0; i < n; i++ {
		s := parts[i]
		if isNewline(s) {
			res[i] = s
			sentenceStart = true
			continue
		}
		s2 := strings.ToLower(s)
		if sentenceStart {
			res[i] = strings.Title(s2)
		} else {
			res[i] = s2
		}
		sentenceStart = endsSendence(s)
	}
	s = strings.Join(res, " ")
	return s
	/*
		parts = strings.FieldsFunc(s, isNewlineChar)
		n = len(parts)
		res = make([]string, n, n)
		for i := 0; i < n; i++ {
			res[i] = strings.Title(res[i])
		}
		return strings.Join(res, "\n")
	*/
}

func panicif(cond bool, args ...interface{}) {
	if !cond {
		return
	}
	msg := "panic"
	if len(args) > 0 {
		s, ok := args[0].(string)
		if ok {
			msg = s
			if len(s) > 1 {
				msg = fmt.Sprintf(msg, args[1:]...)
			}
		}
	}
	panic(msg)
}

func httpErrorf(w http.ResponseWriter, format string, args ...interface{}) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	http.Error(w, msg, http.StatusBadRequest)
}

func bytesToPlane0String(buf []byte) string {
	str := make([]byte, 0, len(buf))
	enc := make([]byte, 3)

	for i := 0; i < len(buf); {
		if buf[i] < 128 {
			str = append(str, buf[i])
			i++
			continue
		}

		ln := 2 * int((buf[i]&0x7f)+1)
		if i+1+ln > len(buf) {
			return ""
		}

		for j := i + 1; j < i+1+ln; j += 2 {
			n := utf8.EncodeRune(enc, rune(buf[j])<<8+rune(buf[j+1]))
			str = append(str, enc[:n]...)
		}

		i += 1 + ln
	}

	return string(str)
}

func plane0StringToBytes(str string) []byte {
	buf := make([]byte, 0, len(str))
	queue := make([]byte, 0, 256)

	appendQueue := func() {
		buf = append(buf, byte(len(queue)/2-1)|0x80)
		buf = append(buf, queue...)
		queue = queue[:0]
	}

	for _, r := range str {
		if r < 128 {
			if len(queue) > 0 {
				appendQueue()
			}
			buf = append(buf, byte(r))
		} else {
			queue = append(queue, byte(r>>8), byte(r))
			if len(queue)/2 == 128 {
				appendQueue()
			}
		}
	}

	if len(queue) > 0 {
		appendQueue()
	}

	return buf
}
