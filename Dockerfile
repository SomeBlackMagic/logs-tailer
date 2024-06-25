# Copyright 2021 The Kubernetes Authors. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM golang:1.21.3-alpine3.18 as builder

RUN apk update \
    && apk upgrade && apk add git

WORKDIR /go/src/k8s.io/SomeBlackMagic/logs-tailer

ARG VERSION
ARG REVISION

COPY . .

RUN go get . && \
    CGO_ENABLED=0 go build -a -installsuffix cgo \
    -ldflags "-s -w" \
    -ldflags="-X 'main.version=${VERSION}'" \
    -ldflags="-X 'main.revision=${REVISION}'" \
    -o logs-tailer .

## Use distroless as minimal base image to package the binary
## Refer to https://github.com/GoogleContainerTools/distroless for more details
# FROM gcr.io/distroless/static:latest

FROM alpine

# COPY --from=busybox:1.35.0-uclibc /bin/sh /bin/sh
# COPY --from=busybox:1.35.0-uclibc /bin/mkdir /bin/mkdir
# COPY --from=busybox:1.35.0-uclibc /bin/chown /bin/chown
# COPY --from=busybox:1.35.0-uclibc /bin/ls /bin/ls
# COPY --from=busybox:1.35.0-uclibc /bin/kill /bin/kill
# COPY --from=busybox:1.35.0-uclibc /bin/echo /bin/echo

# #For debug
# COPY --from=busybox:1.35.0-uclibc /bin/sleep /bin/sleep
# COPY --from=busybox:1.35.0-uclibc /bin/cat /bin/cat
# COPY --from=busybox:1.35.0-uclibc /bin/chmod /bin/chmod

ADD https://www.busybox.net/downloads/binaries/strace_static_x86_64 /bin/strace

RUN /bin/chmod +x /bin/strace

COPY --from=builder /go/src/k8s.io/SomeBlackMagic/logs-tailer/logs-tailer /

CMD ["/logs-tailer", "-folder"]
