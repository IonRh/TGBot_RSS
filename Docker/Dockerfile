FROM debian:12-minimal
RUN apt-get update && \
    apt-get install -y curl sed ntp tzdata && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /root
RUN ls && mkdir /app && cd /app && curl -sL https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/TGBot_RSS.sh | bash
COPY Docker/main.sh /app/main.sh
RUN chmod +x /app/main.sh
ENV TZ=Asia/Shanghai
RUN ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone
# 校准时钟
RUN ntpd -q -p ntp.aliyun.com
ENTRYPOINT ["/app/main.sh"]
