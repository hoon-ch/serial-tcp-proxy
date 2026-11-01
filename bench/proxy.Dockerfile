FROM alpine:3.22
COPY server /server
ENTRYPOINT ["/server"]
