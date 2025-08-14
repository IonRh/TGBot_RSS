FROM frolvlad/alpine-glibc:latest
LABEL org.opencontainers.image.source=https://github.com/IonRh/TGBot_RSS
RUN apk add --no-cache curl bash
USER root
WORKDIR /home/root
RUN curl -sL https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/TGBot_RSS.sh | bash
ENTRYPOINT ["/home/root/TGBot_RSS"]
