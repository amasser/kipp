FROM golang:alpine AS build
RUN apk update && \
	apk add --no-cache git && \
	export GO111MODULE=on && \
	git clone --branch beta https://github.com/uhthomas/kipp /go/src/github.com/uhthomas && \
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o /go/src/github.com/uhthomas/kipp/default/kipp github.com/uhthomas/kipp/cmd/kipp

FROM scratch
COPY --from=build /go/src/github.com/uhthomas/kipp/default /go/src/github.com/uhthomas/kipp/deploy/kipp.cql /
ENTRYPOINT ["/kipp", "--mime", "mime.json", "--path", "/data"]