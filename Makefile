build-docker:
	docker build --build-arg=DOCKER_META_VERSION=dev-dirty --tag logs-tailer:local .

get-deps:
	go get .

build-local:
	CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags "-s -w" -o logs-tailer .
	chmod +x logs-tailer
