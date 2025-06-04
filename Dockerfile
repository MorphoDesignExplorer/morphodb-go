# syntax=docker/dockerfile:1
# multi-stage dockerfile, courtesy of https://docs.docker.com/build/building/multi-stage/
# build image to compile binaries first
FROM golang:1.23-alpine

WORKDIR /app

# copy folder contents into image
COPY . ./

RUN go build -o /bin/main-bin server.go

RUN go build -o /bin/user-manager user-manager/main.go

# rebuild image from scratch

FROM scratch

# copy folder contents into image
COPY . ./ 

COPY --from=0 /bin/main-bin /bin/main-bin

COPY --from=0 /bin/user-manager /bin/user-manager

WORKDIR /

CMD ["/bin/main-bin"]

