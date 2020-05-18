FROM golang:1.14.2-stretch
WORKDIR /root/project
COPY . .
RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -v .

FROM ubuntu
WORKDIR /root/
COPY --from=0 /root/project/frontend .
RUN apt-get update && \
    apt-get install --fix-missing -y ca-certificates && \
		apt-get autoremove -y && \
    apt-get clean && \
		rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
RUN apt-get update && apt
EXPOSE 8080
ENTRYPOINT ["/root/frontend"]