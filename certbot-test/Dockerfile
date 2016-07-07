FROM ubuntu

EXPOSE 5001 5002

RUN apt-get update && apt-get install -y letsencrypt

ENTRYPOINT ["letsencrypt", "--no-verify-ssl", "--non-interactive", "--no-redirect", "--agree-tos", "--register-unsafely-without-email", "--debug", "--http-01-port", "5002", "--tls-sni-01-port", "5001" , "--debug" ]
