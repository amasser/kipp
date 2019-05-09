FROM golang:alpine AS build
RUN apk update && \
	apk add --no-cache git && \
	export GO111MODULE=on && \
	git clone --branch beta https://github.com/uhthomas/kipp /go/src/github.com/uhthomas/kipp && \
	cd /go/src/github.com/uhthomas/kipp && \
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o /kipp /go/src/github.com/uhthomas/kipp/cmd/kipp

FROM scratch
COPY --from=build /kipp /go/src/github.com/uhthomas/kipp/web /
ENTRYPOINT ["/kipp"]