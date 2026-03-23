FROM grafana/k6:latest

USER root
RUN apk add --no-cache postgresql16-client bash jq
USER k6

COPY loadtest.js /scripts/loadtest.js
COPY entrypoint.sh /scripts/entrypoint.sh

ENTRYPOINT ["bash", "/scripts/entrypoint.sh"]
