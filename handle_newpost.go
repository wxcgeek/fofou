// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/fofou/server"
)

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

func handleNewPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024*1024)

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	badRequest := func() { writeSimpleJSON(w, "success", false, "error", "bad-request") }
	internalError := func() { writeSimpleJSON(w, "success", false, "error", "internal-error") }

	var topic server.Topic

	topicID, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("topic")))
	if topicID > 0 {
		if topic = forum.Store.TopicByID(uint32(topicID)); topic.ID == 0 {
			forum.Notice("invalid topic ID: %d\n", topicID)
			badRequest()
			return
		}
	}

	ipAddr := getIPAddress(r)
	user := forum.GetUser(r)
	if forum.Store.IsBlocked(ipAddr) && !user.IsAdmin() {
		forum.Notice("blocked a post from IP: %v", ipAddr)
		badRequest()
		return
	}

	// if user didn't pass the dice test, we will challenge him/her
	if !user.NoTest() {
		recaptcha := strings.TrimSpace(r.FormValue("token"))
		if recaptcha == "" {
			writeSimpleJSON(w, "success", false, "error", "recaptcha-needed")
			return
		}

		resp, err := (&http.Client{Timeout: time.Second * 5}).PostForm("https://www.recaptcha.net/recaptcha/api/siteverify", url.Values{
			"secret":   []string{forum.Recaptcha},
			"response": []string{recaptcha},
		})
		if err != nil {
			forum.Error("recaptcha error: %v", err)
			internalError()
			return
		}

		defer resp.Body.Close()
		buf, _ := ioutil.ReadAll(resp.Body)

		recaptchaResult := map[string]interface{}{}
		json.Unmarshal(buf, &recaptchaResult)

		if r, _ := recaptchaResult["success"].(bool); !r {
			forum.Error("recaptcha failed: %v", string(buf))
			writeSimpleJSON(w, "success", false, "error", "recaptcha-failed")
			return
		}
	}

	subject := strings.Replace(r.FormValue("subject"), "<", "&lt;", -1)
	msg := strings.TrimSpace(r.FormValue("message"))

	// validate the fields
	if !user.IsValid() {
		if forum.NoMoreNewUsers && !topic.FreeReply {
			writeSimpleJSON(w, "success", false, "error", "no-more-new-users")
			return
		}
		copy(user.ID[:], forum.Rand.Fetch(6))
		if user.ID[1] == ':' {
			user.ID[1]++ // in case we get random bytes satrting with "a:"
		}
		if topic.ID == 0 {
			user.N = forum.Rand.Intn(10) + 10
		} else {
			user.N = forum.Rand.Intn(5) + 5
		}
	}

	if user.IsAdmin() && server.AdminOPCode(forum, msg) {
		writeSimpleJSON(w, "success", true, "admin-operation", msg)
		return
	}

	if topic.ID == 0 {
		if tmp := []rune(subject); len(tmp) > forum.MaxSubjectLen {
			tmp[forum.MaxSubjectLen-1], tmp[forum.MaxSubjectLen-2], tmp[forum.MaxSubjectLen-3] = '.', '.', '.'
			subject = string(tmp[:forum.MaxSubjectLen])
		}
	}

	if forum.Store.IsBlocked(user.ID) {
		forum.Notice("blocked a post from user %v", user.ID)
		badRequest()
		return
	}

	image, imageInfo, err := r.FormFile("image")
	if err == nil {
		defer image.Close()
	}

	if err != nil && !strings.Contains(err.Error(), "no such file") {
		writeSimpleJSON(w, "success", false, "error", "image-upload-failed")
		return
	}

	if image != nil && imageInfo != nil && forum.NoImageUpload {
		writeSimpleJSON(w, "success", false, "error", "image-upload-disabled")
		return
	}

	if len(msg) > forum.MaxMessageLen {
		// hard trunc
		msg = msg[:forum.MaxMessageLen]
	}

	if len(msg) < forum.MinMessageLen && image == nil {
		writeSimpleJSON(w, "success", false, "error", "message-too-short")
		return
	}

	if topic.ID > 0 && topic.Locked {
		writeSimpleJSON(w, "success", false, "error", "topic-locked")
		return
	}

	forum.SetUser(w, user)

	imagePath := ""
	if image != nil && imageInfo != nil {
		ext, hash := strings.ToLower(filepath.Ext(imageInfo.Filename)), sha1.Sum([]byte(imageInfo.Filename))
		if ext != ".png" && ext != ".gif" && ext != ".jpg" && ext != ".jpeg" {
			writeSimpleJSON(w, "success", false, "error", "image-invalid-format")
			return
		}
		t := time.Now().Format("2006-01-02")
		imagePath = fmt.Sprintf("%s/%x%s", t, hash, ext)
		os.MkdirAll("data/images/"+t, 0755)
		if of, err := os.Create("data/images/" + imagePath); err == nil {
			defer of.Close()
			io.Copy(of, image)
			iq.Push("data/images/" + imagePath)
		} else {
			writeSimpleJSON(w, "success", false, "error", "image-disk-error")
			forum.Error("copy image to dest: %v", err)
			return
		}
	}

	if topic.ID == 0 {
		topicID, err := forum.Store.NewTopic(subject, msg, imagePath, user.ID, ipAddr)
		if err != nil {
			forum.Error("failed to create new topic: %v", err)
			internalError()
			return
		}
		writeSimpleJSON(w, "success", true, "topic", topicID)
		return
	}

	if err := forum.Store.NewPost(topic.ID, msg, imagePath, user.ID, ipAddr); err != nil {
		forum.Error("failed to create new post to %d: %v", topic.ID, err)
		internalError()
		return
	}
	writeSimpleJSON(w, "success", true, "topic", topic.ID)
}
