# Use official HAProxy with our config layout baked as defaults.
FROM haproxy:3.0-alpine

USER root
RUN mkdir -p /var/run/haproxy /etc/haproxy/backends.d /etc/haproxy/certs \
    && chown -R haproxy:haproxy /var/run/haproxy /etc/haproxy/backends.d

COPY haproxy.cfg /usr/local/etc/haproxy/haproxy.cfg
COPY backends.d/ /etc/haproxy/backends.d/

# Master-worker mode for SIGUSR2 soft-reloads.
# Second -f loads managed backend sections from backends.d/*.cfg
CMD ["haproxy", "-W", "-db", "-f", "/usr/local/etc/haproxy/haproxy.cfg", "-f", "/etc/haproxy/backends.d"]
