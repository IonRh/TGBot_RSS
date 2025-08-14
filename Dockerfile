FROM debian:12-slim
LABEL org.opencontainers.image.source=https://github.com/IonRh/TGBot_RSS
RUN apt-get update && \
    apt-get install -y curl && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
USER root
WORKDIR /home/root
RUN curl -sL https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/TGBot_RSS.sh | bash
ENTRYPOINT ["/home/root/TGBot_RSS"]
