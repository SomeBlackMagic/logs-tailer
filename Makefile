build-local:
	docker build -t logs-tailer:local .

get-deps:
	go get .

build-app:
	CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags "-s -w" -o logs-tailer .
	chmod +x logs-tailer
