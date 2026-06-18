FROM python:3.11-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
        libffi-dev \
        openssh-client \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/netconf-mock
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY netconf_server.py .
RUN ssh-keygen -t rsa -b 2048 -f /opt/netconf-mock/netconf_rsa.key -N ""

# 830: NETCONF SSH port, 8088: HTTP control API
EXPOSE 830/tcp
EXPOSE 8088/tcp

CMD ["python", "netconf_server.py"]
