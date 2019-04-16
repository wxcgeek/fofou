## Get the code
```
go get github.com/coyove/fofou
```

## Overview

Fofou (Forums For You) is a simple forum software inspired by Joel On Software forum software (http://discuss.joelonsoftware.com/?joel).

Fofou2 is a refactored version of fofou with more functions. It is now powering https://sserr.net

## Run

Simply run:
```
go run main.go -s SECRET_PASSWORD
```
Fofou2 will run in test mode if not provided with a `SECRET_PASSWORD` using `-s`.

## Admin Cookie

After launching fofou2 you can navigate to `http://.../cookie`, enter `SECRET_PASSWORD` in the first textbox and `ADMIN_NAME,255` in the second textbox.

You will receive the admin cookie `ADMIN_NAME` after submitting the form. (`255` means full privilege)

## Snapshot

Fofou2's main database is named as `data/main.txt`. Since it's an append-only log file, it will become very large eventually. You can run:
```
go run main.go -ss main.txt.ss
```
to snapshot the data to `main.txt.ss`, and use which to replace `data/main.txt` for faster replaying.

## Recaptcha

To use Google Recaptcha service, setup these environment variables before launching fofou2:
```
export f2_token=SITE_KEY
export f2_secret=SECRET_KEY
```

## Backup

All data are stored in `data` directory.
