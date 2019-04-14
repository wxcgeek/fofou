function _ref(el, id) {
    if ($(el).prop('error')) return;
    if ($(el).prop('expanded')) {
        $(el).prop("expanded", false).next().remove();
        return;
    }
    $(el).prop("expanded", true);
    $.get("/p/" + id + "?raw=1", function (data) {
        var div = $("<div>").addClass('ref');
        div.html(data);
        $(el).after(div);
    }).error(function() {
        $(el).append(" (错误)").prop("error", true);
    });
}

function _enlarge(el, url) {
    var m = localStorage.getItem("image-view") || 'expand';
    if (m === 'expand') {
        if ($(el).prop('loading')) return;
        if ($(el).prop('expanded')) {
            $(el).addClass('image').removeClass('image-large').prop('expanded', false);
        } else {
            $(el).removeClass('image').addClass('image-large').prop('expanded', true);
            if ($(el).prop('loaded')) return;

            $(el).prop('loading', true).prop('loaded', false);
            var xhr = new XMLHttpRequest();
            var progress = $(el).parent().find('.loading').first();
            xhr.open('GET', url);
            xhr.responseType = 'arraybuffer';
            xhr.onreadystatechange = function() {
                if (xhr.readyState === 4) {
                    var blob = new Blob([xhr.response]);
                    $(el).attr('src', window.URL.createObjectURL(blob)).prop('loading', false).prop('loaded', true);
                    progress.text('');
                }
            };
            xhr.onprogress = function(e) { progress.text(parseInt((e.loaded / e.total) * 100) + "%"); };
            xhr.onloadstart = function() { progress.text("0%"); };
            xhr.onerror = function() { progress.text("加载失败"); };
            xhr.send();
        }
    } else {
        window.open(url);
    }
}

function _togglePost(id) {
    var p = $("#post-" + id + " .toggle").first();
    if (!p.get(0)) return;

    var c = p.prop("fold");
    var m = $("#post-" + id + " .message").first();
    var s = JSON.parse(localStorage.getItem("fold") || '{}');
    var cls = (localStorage.getItem("hide-looks") || 'gray') == 'gray' ? 'fold' : 'fold fold-hide';

    p.prop("fold", !c).removeClass("icon-plus-squared icon-minus-squared").addClass(c ? "icon-minus-squared" : "icon-plus-squared");
    if (c) {
        m.show().parent().removeClass(cls);
        delete s[id];
    } else {
        m.hide().parent().addClass(cls);
        s[id] = new Date().getTime();
    }
    localStorage.setItem("fold", JSON.stringify(s));
}

function _submit(btn, msg, callback) {
    btn ? $(btn).attr('disabled', 'true') : 0;
    var form = new FormData();
    var options = $('#options').val();
    if (msg) {
        form.append('message', msg);
    } else {
        form.append('subject', $('#subject').val());
        form.append('message', $('#message').val());
        form.append('image', $('#select-image').get(0).files[0]);
        form.append('topic', window.TOPIC_ID || 0);
        form.append('uuid', $('#newpost').attr('uuid'));
        form.append('options', options);
        try {
            form.append('token', grecaptcha.getResponse());
        } catch (ex) {}
    }
    var xhr = new XMLHttpRequest();
    xhr.onreadystatechange = function(e) {
        if ( 4 == this.readyState ) {
            try {
                var resp = xhr.responseText;
                resp = JSON.parse(resp);
                if (resp.success) {
                    localStorage.setItem("options", options ? options : "");
                    if (callback) {
                        callback();
                    } else {
                        resp.longid && (options || "").indexOf("nonoko") == -1
                            ? location.href = "/p/" + resp.longid : location.reload();
                    }
                    return;
                }
                alert("发生错误：\ncode: " + resp.error + "\n" + ({
                    "bad-request": "无效请求 ",
                    "internal-error": "内部错误",
                    "recaptcha-needed": "请完成验证",
                    "recaptcha-failed": "验证失败，请刷新页面重试",
                    "no-more-new-users": "未持有cookie的匿名用户无法发言",
                    "message-too-short": "正文内容过短",
                    "topic-locked": "主题已被锁定",
                    "image-upload-failed": "图片上传失败",
                    "image-upload-disabled": "禁止上传图片",
                    "image-invalid-format": "图片格式不支持",
                    "image-disk-error": "图片上传失败",
                })[resp.error]);
                $("#newpost").attr("uuid", 'xxxxxxxxxxxx4xxxyxxxxxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
                    var r = Math.random() * 16 | 0, v = c == 'x' ? r : (r & 0x3 | 0x8);
                    return v.toString(16);
                }));
            } catch (ex) {
                alert("发生错误：\n" + ex);
                console.error(resp);
            }
        }
        btn ? $(btn).removeAttr('disabled') : 0;
    };
    xhr.open('post', "/api");
    xhr.send(form);
}

function _dropdownHeight(el) {
    el = $(el).find("div");
    var diff = el.height() + el.offset().top - $(window).scrollTop() - $(window).height();
    if (diff > 0) el.css('height', el.height() - diff - 5).css('overflow-y', 'scroll');
}

function _openpgpSign(privkey, passphrase, text, callback) {
    var sign = function() {
        openpgp.key.readArmored(privkey).then(function(data) {
            var privKeyObj = data.keys[0];
            var sign = function() {
                var options = {
                    message: openpgp.cleartext.fromText(text),
                    privateKeys: [privKeyObj]                
                };
                openpgp.sign(options).then(callback);
            };
            privKeyObj.decrypt(passphrase).then(sign).catch(sign);
        });
    };

    if (!window.openpgp) {
        $.getScript("/s/openpgp.min.js", sign);
    } else {
        sign();
    }
}
