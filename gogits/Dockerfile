FROM golang:1.8.3-alpine3.5

# Install system utils & Gogs runtime dependencies
ADD ./gogits/gosu-amd64 /usr/sbin/gosu
RUN  chmod +x /usr/sbin/gosu \
  && echo http://mirrors.ustc.edu.cn/alpine/v3.5/main > /etc/apk/repositories \
  && echo http://mirrors.ustc.edu.cn/alpine/v3.5/community >> /etc/apk/repositories \
  && apk --no-cache --no-progress add \
      bash \
      ca-certificates \
      curl \
      git \
      linux-pam \
      openssh \
      s6 \
      shadow \
      socat \
      tzdata \
   && mkdir -p $GOPATH/src/github.com/jteeuwen \
   && go get github.com/jteeuwen/go-bindata \
   && cd $GOPATH/src/github.com/jteeuwen/go-bindata/go-bindata \
   && go build -o /usr/local/bin/go-bindata \
   && cd -


ENV GOGS_CUSTOM /data/gogs

# Configure LibC Name Service
COPY ./docker/nsswitch.conf /etc/nsswitch.conf
COPY ./docker /app/gogs/docker
COPY ./templates /app/gogs/templates
COPY ./public /app/gogs/public

WORKDIR /app/gogs/build
COPY . .
COPY ./gogits/build.sh ./docker/build.sh

RUN   chmod u+x ./docker/build.sh \
   && ./docker/build.sh \
   && ./docker/finalize.sh

# Configure Docker Container
VOLUME ["/data"]
EXPOSE 22 3000

ENTRYPOINT ["/app/gogs/docker/start.sh"]

CMD ["/bin/s6-svscan", "/app/gogs/docker/s6/"]
