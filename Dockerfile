FROM golang:1.26-alpine AS build-env

WORKDIR /app
ADD ./go.mod ./go.sum /app/
RUN cd /app && go mod download && \
ORCH_VERSION=$(go list -m -f '{{.Version}}' github.com/DataDog/orchestrion) && \
go install github.com/DataDog/orchestrion@${ORCH_VERSION}
ADD . /app
RUN cd /app && CGO_ENABLED=0 orchestrion go build -o goserver

FROM alpine
RUN apk update && apk add ca-certificates tzdata && rm -rf /var/cache/apk/*
WORKDIR /app
COPY --from=build-env /app/goserver /app

EXPOSE 8001

RUN adduser -s /bin/sh --disabled-password -u 1000 go-user

RUN chown -R go-user:go-user /app

USER 1000

ENTRYPOINT ["./goserver"]
