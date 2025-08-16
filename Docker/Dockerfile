FROM alpine:3.20
RUN apk add --no-cache curl bash sed busybox
WORKDIR /root
RUN mkdir /app && cd /app && curl -sL https://ghproxy.badking.pp.ua/https://raw.githubusercontent.com/IonRh/TGBot_RSS/main/TGBot_RSS.sh | bash
RUN chmod +x /app/main.sh && export TZ='Asia/Shanghai'
# 校准时钟
RUN busybox ntpd -q -p ntp.aliyun.com
ENTRYPOINT ["/app/main.sh"]
