FROM nginx:1.27-alpine

RUN rm -f /etc/nginx/conf.d/default.conf     && mkdir -p /var/cache/nginx /var/run     && chown -R nginx:nginx /var/cache/nginx /var/run

COPY nginx.prod.conf /etc/nginx/nginx.conf

USER nginx
EXPOSE 8080
ENTRYPOINT ["nginx", "-g", "daemon off;"]
