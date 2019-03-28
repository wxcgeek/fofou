// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"bufio"
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

func throtNewPost(ip, id [8]byte) bool {
	now := time.Now().Unix()
	ts, ok := throtIPID.Get(ip)
	if !ok {
		ts, ok = throtIPID.Get(id)
		if !ok {
			throtIPID.Add(ip, now)
			throtIPID.Add(id, now)
			return true
		}
	}
	t := ts.(int64)
	if now-t > int64(forum.Cooldown) {
		throtIPID.Add(ip, now)
		throtIPID.Add(id, now)
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
	const needle = "\\/:*?\"<>|"
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

func handleNewPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(forum.MaxImageSize)*1024*1024)

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	badRequest := func() { writeSimpleJSON(w, "success", false, "error", "bad-request") }
	internalError := func() { writeSimpleJSON(w, "success", false, "error", "internal-error") }

	var topic server.Topic

	topicID, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("topic")))
	if topicID > 0 {
		if topic = forum.Store.GetTopic(uint32(topicID), server.DefaultTopicFilter); topic.ID == 0 {
			forum.Notice("invalid topic ID: %d\n", topicID)
			badRequest()
			return
		}
	}

	ipAddr, user := getIPAddress(r), forum.GetUser(r)

	if !user.Can(server.PERM_ADMIN) {
		if forum.Store.IsBlocked(ipAddr) {
			forum.Notice("blocked a post from IP: %v", ipAddr)
			badRequest()
			return
		}
		if forum.Store.IsBlocked(user.ID) {
			forum.Notice("blocked a post from user %v", user.ID)
			badRequest()
			return
		}
		if !user.CanModerate() && !throtNewPost(ipAddr, user.ID) {
			badRequest()
			return
		}
	}

	if !user.IsValid() {
		if forum.NoMoreNewUsers && !topic.FreeReply {
			writeSimpleJSON(w, "success", false, "error", "no-more-new-users")
			return
		}
		copy(user.ID[:], forum.Rand.Fetch(6))
		if user.ID[0] == '^' {
			user.ID[0]++ // ^ means mod
		}
		user.T = time.Now().Unix()
		if topic.ID == 0 {
			user.N = uint32(forum.Rand.Intn(10) + 10)
		} else {
			user.N = uint32(forum.Rand.Intn(5) + 5)
		}
	}

	// if user didn't pass the dice test, we will challenge him/her
	if !forum.NoRecaptcha && !user.Can(server.PERM_NO_ROLL) && !user.PassRoll() {
		_testCount, _ := badUsers.Get(user.ID)
		testCount, _ := _testCount.(int)
		if testCount++; testCount > 10 {
			badUsers.Remove(user.ID)
			forum.Block(user.ID)
			forum.Block(ipAddr)
			badRequest()
			return
		}

		recaptcha := strings.TrimSpace(r.FormValue("token"))
		if recaptcha == "" {
			writeSimpleJSON(w, "success", false, "error", "recaptcha-needed")
			badUsers.Add(user.ID, testCount)
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
			badUsers.Add(user.ID, testCount)
			writeSimpleJSON(w, "success", false, "error", "recaptcha-failed")
			return
		}
	}
	badUsers.Remove(user.ID)

	subject := strings.Replace(r.FormValue("subject"), "<", "&lt;", -1)
	msg := strings.TrimSpace(r.FormValue("message"))
	sage := r.FormValue("sage") != "" && user.Posts >= user.N

	// validate the fields

	if modCode(forum, user, msg) {
		_, username := server.Format8Bytes(user.ID)
		ipstr, _ := server.Format8Bytes(ipAddr)
		forum.Notice("mod %s from %s has performed: %s", username, ipstr, msg)
		writeSimpleJSON(w, "success", true, "mod-operation", msg)
		return
	}

	// simple mechanism to prevent double post only
	uuid := server.DecodeUUID(r.FormValue("uuid"))
	if _, existed := uuids.Get(uuid); existed {
		badRequest()
		return
	}
	uuids.Add(uuid, true)

	if topic.ID == 0 {
		if tmp := []rune(subject); len(tmp) > forum.MaxSubjectLen {
			tmp[forum.MaxSubjectLen-1], tmp[forum.MaxSubjectLen-2], tmp[forum.MaxSubjectLen-3] = '.', '.', '.'
			subject = string(tmp[:forum.MaxSubjectLen])
		}
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

	var aImage *server.Image
	if image != nil && imageInfo != nil {
		aImage = &server.Image{}

		ext, hash := strings.ToLower(filepath.Ext(imageInfo.Filename)), sha1.Sum([]byte(imageInfo.Filename))
		if ext != ".png" && ext != ".gif" && ext != ".jpg" && ext != ".jpeg" {
			writeSimpleJSON(w, "success", false, "error", "image-invalid-format")
			return
		}

		t := time.Now().Format("2006-01-02/15")
		aImage.Name = sanitizeFilename(imageInfo.Filename)
		aImage.Path = fmt.Sprintf("%s/%s_%x%s", t, aImage.Name, hash[:4], ext)
		os.MkdirAll(DATA_IMAGES+t, 0755)

		of, err := os.Create(DATA_IMAGES + aImage.Path)
		if err != nil {
			writeSimpleJSON(w, "success", false, "error", "image-disk-error")
			forum.Error("copy image to dest: %v", err)
			return
		}

		nw, _ := io.Copy(of, image)
		aImage.Size = uint32(nw)
		iq.Push(DATA_IMAGES + aImage.Path)
		of.Close()
	}

	var postLongID uint64
	if topic.ID == 0 {
		postLongID, err = forum.Store.NewTopic(subject, msg, aImage, user.ID, ipAddr)
		if err != nil {
			forum.Error("failed to create new topic: %v", err)
			internalError()
			return
		}
		topicID, _ := server.SplitID(postLongID)
		if sage {
			forum.Store.OperateTopic(topicID, server.OP_SAGE)
		}
	} else {
		postLongID, err = forum.Store.NewPost(topic.ID, msg, aImage, user.ID, ipAddr)
		if err != nil {
			forum.Error("failed to create new post to %d: %v", topic.ID, err)
			internalError()
			return
		}
	}

	tmpt, tmpp := server.SplitID(postLongID)
	writeSimpleJSON(w, "success", true, "topic", tmpt, "post", tmpp)
}

func modCode(forum *server.Forum, u server.User, msg string) bool {
	r := bufio.NewReader(strings.NewReader(msg))
	opcode := false
	for {
		line, _, err := r.ReadLine()
		if err != nil {
			break
		}

		msg := string(line)
		if !strings.HasPrefix(msg, "!!") {
			break
		}

		eidx := strings.Index(msg, "=")
		if eidx == -1 {
			break
		}

		v := msg[eidx+1:]
		vint, _ := strconv.ParseInt(v, 10, 64)
		switch msg[2:eidx] {
		case "moat":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			switch v {
			case "cookie":
				forum.NoMoreNewUsers = !forum.NoMoreNewUsers
			case "image":
				forum.NoImageUpload = !forum.NoImageUpload
			case "recaptcha":
				forum.NoRecaptcha = !forum.NoRecaptcha
			}
			opcode = true
		case "max-message-len":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			forum.MaxMessageLen = int(vint)
			opcode = true
		case "max-subject-len":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			forum.MaxSubjectLen = int(vint)
			opcode = true
		case "search-timeout":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			forum.SearchTimeout = int(vint)
			opcode = true
		case "cooldown":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			forum.Cooldown = int(vint)
			opcode = true
		case "max-image-size":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			forum.MaxImageSize = int(vint)
			opcode = true
		case "delete":
			res := forum.Store.DeletePost(u, uint64(vint), func(img string) {
				os.Remove(DATA_IMAGES + img)
				os.Remove(DATA_IMAGES + img + ".thumb.jpg")
			})
			opcode = true
			if res != nil {
				break
			}
		case "stick":
			if !u.Can(server.PERM_STICKY_PURGE) {
				return true
			}
			forum.Store.OperateTopic(uint32(vint), server.OP_STICKY)
			opcode = true
		case "lock":
			if !u.Can(server.PERM_LOCK_SAGE_DELETE) {
				return true
			}
			forum.Store.OperateTopic(uint32(vint), server.OP_LOCK)
			opcode = true
		case "purge":
			if !u.Can(server.PERM_STICKY_PURGE) {
				return true
			}
			forum.Store.OperateTopic(uint32(vint), server.OP_PURGE)
			opcode = true
		case "free-reply":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			forum.Store.OperateTopic(uint32(vint), server.OP_FREEREPLY)
			opcode = true
		case "sage":
			if !u.Can(server.PERM_LOCK_SAGE_DELETE) {
				return true
			}
			forum.Store.OperateTopic(uint32(vint), server.OP_SAGE)
			opcode = true
		case "block":
			if !u.Can(server.PERM_BLOCK) {
				return true
			}
			forum.Store.Block(server.Parse8Bytes(v))
			opcode = true
		case "title":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			forum.Title = v
			opcode = true
		case "url":
			if !u.Can(server.PERM_ADMIN) {
				return true
			}
			forum.URL = v
			opcode = true
		}
	}

	if opcode {
		buf, _ := json.Marshal(forum.ForumConfig)
		ioutil.WriteFile(DATA_CONFIG, buf, 0755)
	}

	return opcode
}
