go run -ldflags="-X main.version=$(git log --pretty=format:'%h' -n 1)" main.go "$@"
