package markup

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

var test, debug bool

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func lastHR(buf *bytes.Buffer) bool {
	if buf.Len() >= 4 {
		p, i := buf.Bytes(), buf.Len()-4
		return p[i] == '<' && p[i+1] == 'h' && p[i+2] == 'r' && p[i+3] == '>'
	}
	return false
}

func Do(in string, allowHTML bool, maxLength int) string {
	if test && strings.HasPrefix(in, "DBG") {
		debug = true
		in = in[3:]
	}

	out := bytes.Buffer{}
	idx, tmp := 0, [4]byte{}

	prefetch3 := func() (a rune, b rune, c rune) {
		i, ln := idx, 0
		if a, ln = utf8.DecodeRuneInString(in[i:]); a == utf8.RuneError {
			return
		}
		i += ln
		if b, ln = utf8.DecodeRuneInString(in[i:]); b == utf8.RuneError {
			return
		}
		i += ln
		c, ln = utf8.DecodeRuneInString(in[i:])
		return
	}

	var inCode bool
	var inCodeHTML bool
	var inRef *uint64
	var inLink *bytes.Buffer
	var inLinkPos int

	ignore := 0
	for ir, r := range in {
		idx += utf8.EncodeRune(tmp[:], r)

		if inLink != nil && r != ']' && r != '[' && r != '`' {
			inLink.WriteRune(r)
		}

		if ignore > 0 {
			ignore--
			if debug {
				fmt.Println("  ignore:", string(r))
			}
			continue
		}

		if maxLength > 0 && ir > maxLength {
			break
		}

		if inRef != nil {
			if isDigit(r) {
				*inRef = *inRef*10 + uint64(r-'0')
				continue
			} else {
				if *inRef == 0 {
					// >>abc
					out.WriteString("&gt;&gt;")
				} else {
					if test {
						out.WriteString(fmt.Sprintf("#TEST#%d#TEST#", *inRef))
					} else {
						out.WriteString(fmt.Sprintf("<a href='javascript:void(0)' onclick='_ref(this,%d)'>&gt;&gt;%d</a>", *inRef, *inRef))
					}
				}
				inRef = nil
				// fall through to handle 'r'
			}
		}

		if debug {
			x := string(r)
			if inCode {
				x += " [C]"
				if inCodeHTML {
					x += " [H]"
				}
			}
			fmt.Println("at", idx, ":", x)
		}

		switch r {
		case ' ':
			if inCode {
				out.WriteRune(r)
			} else {
				out.WriteString("&nbsp;")
			}
		case '\n':
			if inCode {
				out.WriteRune(r)
			} else if !lastHR(&out) {
				out.WriteString("<br>")
			}
		case '<':
			if inCodeHTML {
				out.WriteRune(r)
			} else {
				out.WriteString("&lt;")
			}
		case '>':
			if inCodeHTML {
				out.WriteRune(r)
			} else {
				r2, r3, _ := prefetch3()
				if r2 == '>' && isDigit(r3) && !inCode {
					// >>12345
					inRef = new(uint64)
					ignore = 1
				} else {
					out.WriteString("&gt;")
				}
			}
		case '[':
			if inCode {
				out.WriteRune(r)
			} else if inLink != nil {
				out.Truncate(inLinkPos)
				out.WriteRune('[')
				out.WriteString(Do(inLink.String(), allowHTML, 0))
				inLink.Reset()
				inLinkPos = out.Len()
			} else {
				inLink = &bytes.Buffer{}
				inLinkPos = out.Len()
			}
		case ']':
			if inCode || inLink == nil {
				out.WriteRune(r)
			} else {
				out.Truncate(inLinkPos)
				x := inLink.String()
				u, err := url.Parse(x)
				if err == nil && strings.HasPrefix(x, "http") {
					x = u.String()
					if test {
						out.WriteString(fmt.Sprintf("#TEST#%s#TEST#", x))
					} else {
						out.WriteString(fmt.Sprintf("<a href='%s' target='_blank'>%s</a>", x, x))
					}
				} else {
					out.WriteRune('[')
					out.WriteString(Do(inLink.String(), allowHTML, 0))
					out.WriteRune(']')
				}
				inLink = nil
			}
		case '=':
			r2, r3, r4 := prefetch3()
			if r2 == r && r3 == r && r4 == r && !inCode {
				out.WriteString("<hr>")
				ignore = 3
			} else {
				out.WriteRune(r)
			}
		case '`':
			r2, r3, r4 := prefetch3()
			if inLink != nil {
				out.Truncate(inLinkPos)
				out.WriteRune('[')
				out.WriteString(Do(inLink.String(), allowHTML, 0))
				inLink = nil // ` is not a valid char in URL
			}

			if r2 == r3 && r2 == r {
				if r4 == r && allowHTML {
					if inCode == inCodeHTML {
						// must be identical
						inCode = !inCode
						inCodeHTML = !inCodeHTML
						ignore = 3
						continue
					}
				}

				if inCode {
					inCode = false
					out.WriteString("</code>")
				} else {
					inCode = true
					out.WriteString("<code>")
				}
				ignore = 2
			} else {
				out.WriteRune(r)
			}
		default:
			out.WriteRune(r)
		}
	}

	if inCode && !inCodeHTML {
		// not good, try to fix it
		out.WriteString("</code>")
	}

	if inLink != nil {
		out.Truncate(inLinkPos)
		out.WriteRune('[')
		out.WriteString(Do(inLink.String(), allowHTML, 0))
	}

	return out.String()
}
