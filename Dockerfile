# Build Geth in a stock Go builder container
FROM ubuntu:20.04 as builder

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -yq golang gcc git make #linux-headers

ADD . /go-ethereum
RUN cd /go-ethereum && make geth

# Pull Geth into a second stage deploy alpine container
FROM ubuntu:20.04

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -yq ca-certificates
COPY --from=builder /bor/build/bin/bor /usr/local/bin/
RUN ln -s /usr/local/bin/bor /usr/local/bin/geth

EXPOSE 8545 8546 8547 30303 30303/udp
ENTRYPOINT ["bor"]

