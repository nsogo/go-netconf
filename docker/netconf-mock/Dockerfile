FROM python:3.11-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
        libffi-dev \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/netconf-mock
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY netconf_server.py .
COPY netconf_rsa.key .

# 830: NETCONF SSH port, 8088: HTTP control API
EXPOSE 830/tcp
EXPOSE 8088/tcp

CMD ["python", "netconf_server.py"]
