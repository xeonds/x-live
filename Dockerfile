FROM tiangolo/nginx-rtmp

RUN apt update && apt install ffmpeg -y

LABEL maintainer="xero"

# Expose ports
EXPOSE 1935 80

CMD ["nginx", "-g", "daemon off;"]