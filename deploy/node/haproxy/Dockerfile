# hapanel node HAProxy — prepares /var/run/haproxy so admin.sock works with a
# shared named volume (official image alone often fails: non-root + empty vol).
FROM haproxy:3.0-alpine

USER root
COPY docker-entrypoint.sh /usr/local/bin/hapanel-haproxy-entrypoint.sh
RUN chmod +x /usr/local/bin/hapanel-haproxy-entrypoint.sh \
    && mkdir -p /var/run/haproxy /etc/haproxy/backends.d /etc/haproxy/certs \
    && chmod 777 /var/run/haproxy

# Stay root so the runtime volume mount is writable for the stats socket.
USER root
ENTRYPOINT ["/usr/local/bin/hapanel-haproxy-entrypoint.sh"]
CMD ["-W", "-db", "-f", "/etc/haproxy/backends.d"]
