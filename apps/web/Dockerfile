FROM node:22-alpine AS build

WORKDIR /app
COPY package.json ./
RUN npm install --no-audit --no-fund

COPY tsconfig.json index.html ./
COPY src ./src
RUN npm run build

FROM nginx:1.27-alpine

RUN rm -f /etc/nginx/conf.d/default.conf     && mkdir -p /var/cache/nginx /var/run     && chown -R nginx:nginx /var/cache/nginx /var/run /usr/share/nginx/html

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build --chown=nginx:nginx /app/dist /usr/share/nginx/html

USER nginx
EXPOSE 8080
