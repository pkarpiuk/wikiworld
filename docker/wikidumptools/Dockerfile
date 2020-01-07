FROM ubuntu:18.04
ENV DEBIAN_FRONTEND noninteractive
COPY install.sh install2.sh /root/
RUN /root/install.sh
ENV LC_ALL pl_PL.UTF-8
ENV LANGUAGE pl_PL:pl
ENV LANG pl_PL.UTF-8
RUN /root/install2.sh
USER ubuntu
COPY --chown=ubuntu install3.sh code README.txt wiki-extra-download.rb start.sh create-data-dir-hierarchy.sh worker.rb /home/ubuntu/
ENV GOPATH /home/ubuntu
WORKDIR /home/ubuntu
RUN /home/ubuntu/install3.sh
ENV PATH /usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENTRYPOINT ["./start.sh"]


