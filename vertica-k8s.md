# Setup Vertica Cluster on Kubernetes
```text
                            DQ 2021.05.05
```
Vertica is columnar MPP analytic database, separation computing from storage of its cloud-native Eon mode makes it elastic very well on Kubernetes platform. 

Eon mode need a communal storage, so at first we install Minio which is in official support list  of Vertica.

## Install Minio on k8s

### step 1: install minio

#### option 1.1: install online

```BASH
helm repo remove minio
helm repo add minio https://helm.min.io/
helm repo update
helm inspect values minio/minio

# helm delete minio-release # delete former installation
helm install --debug --set service.type=NodePort,service.nodePort=9000,accessKey=dbadmin,secretKey=verticas3,resources.requests.memory=1Gi,replicas=1 minio/minio
```

#### option 1.2: install offline

Maybe the environment is not connected with Internet, we can download related docker images and Helm Chart when Internet is accessible, and install them offline.

```BASH
# download docker image
#docker login ; docker pull minio/minio:RELEASE.2021-02-14T04-01-33Z ; docker image save minio/minio:RELEASE.2021-02-14T04-01-33Z | gzip > minio-minio-RELEASE.2021-02-14T04-01-33Z.tgz
#docker pull minio/mc:RELEASE.2021-02-14T04-28-06Z ; docker image save minio/mc:RELEASE.2021-02-14T04-28-06Z | gzip > minio-mc-RELEASE.2021-02-14T04-28-06Z.tgz
# download Helm Chart
wget https://helm.min.io/releases/minio-8.0.10.tgz

# load images to local registry or docker of every node
gunzip -c minio-minio-RELEASE.2021-02-14T04-01-33Z.tgz | minikube ssh --native-ssh=false docker image load # minikube image load, or docker image load for ssh each node
minikube ssh docker image ls minio/minio
gunzip -c minio-mc-RELEASE.2021-02-14T04-28-06Z.tgz | minikube ssh --native-ssh=false docker image load # minikube image load, or docker image load for ssh each node
minikube ssh docker image ls minio/mc

# helm delete minio-release # delete former installation
helm install --debug --set service.type=NodePort,service.nodePort=9000,accessKey=dbadmin,secretKey=verticas3,resources.requests.memory=1Gi,replicas=1 minio-release minio-8.0.10.tgz
```

### step 2: test minio

```BASH
mc config host add local http://localhost:9000 dbadmin verticas3
mc mb local/test
seq 1 10 | mc pipe local/test/test.cs
mc cat local/test/test.cs | wc -l
```

## Install Vertica on k8s

### step 1: install Vertica

####   option 1.1: install online from GitHub & DockerHub

```BASH
helm repo add vertica-charts https://vertica.github.io/charts
helm repo update
helm inspect values vertica-charts/vertica
docker login
kubectl create secret generic regcred --from-file=.dockerconfigjson=$HOME/.docker/config.json --type=kubernetes.io/dockerconfigjson 
kubectl get secret regcred --output=yaml

# helm delete vertica-release # delete former installation
helm install --debug --set subclusters.defaultsubcluster.service.type=NodePort,subclusters.defaultsubcluster.service.nodePort=5433,subclusters.defaultsubcluster.replicaCount=3,subclusters.defaultsubcluster.resources.requests.cpu=1,subclusters.defaultsubcluster.resources.requests.memory=1Gi,subclusters.defaultsubcluster.resources.limits.cpu=1,subclusters.defaultsubcluster.resources.limits.memory=1Gi vertica-release vertica-charts/vertica
```

####   option 1.2: from offline

Maybe the environment is not connected with Internet, we can download related docker image and Helm Chart when Internet is accessible, and install them offline.

```BASH
# download docker image
#docker login ; docker pull verticadocker/vertica-k8s ; docker image save verticadocker/vertica-k8s | gzip > verticadocker-vertica-k8s.tgz
# download Helm Chart
wget https://github.com/vertica/vertica-kubernetes/releases/download/v0.1.0/vertica-0.1.0.tgz

# load image to local registry or docker of every node
gunzip -c verticadocker-vertica-k8s.tgz | minikube ssh --native-ssh=false docker image load # minikube image load, or docker image load for ssh each node
minikube ssh docker image ls verticadocker/vertica-k8s

# helm delete vertica-release # delete former installation
helm install --debug --set subclusters.defaultsubcluster.service.type=NodePort,subclusters.defaultsubcluster.service.nodePort=5433,subclusters.defaultsubcluster.replicaCount=3,subclusters.defaultsubcluster.resources.requests.cpu=1,subclusters.defaultsubcluster.resources.requests.memory=1Gi,subclusters.defaultsubcluster.resources.limits.cpu=1,subclusters.defaultsubcluster.resources.limits.memory=1Gi vertica-release vertica-0.1.0.tgz 

helm get values vertica-release
kubectl get events -w
kubectl get svc -o wide
kubectl get pods -o wide # kubectl exec -it $POD_NAME -- /bin/bash
kubectl describe pod $POD_NAME
```

### step 2: setup Vertica cluster

```BASH
RELEASE=vertica-release
SELECTOR=vertica.com/usage=server,app.kubernetes.io/name=vertica,app.kubernetes.io/instance=$RELEASE
NAMESPACE=default
ALL_HOSTS=$(kubectl get pods -n $NAMESPACE --selector=$SELECTOR -o=jsonpath='{range .items[*]}{.metadata.name}.{.spec.subdomain},{end}' | sed 's/.$//')
POD_NAME=$(kubectl get pods -n $NAMESPACE --selector=$SELECTOR -o jsonpath="{.items[0].metadata.name}")

kubectl exec $POD_NAME -i -n $NAMESPACE -- sudo /opt/vertica/sbin/install_vertica \
    --license /home/dbadmin/licensing/ce/vertica_community_edition.license.key \
    --accept-eula \
    --hosts $ALL_HOSTS \
    --dba-user-password-disabled \
    --failure-threshold NONE \
    --no-system-configuration \
    --point-to-point \
    --data-dir /home/dbadmin/local-data/data
```

### step 3: create database

#### option 3.1: create Eon mode database

```BASH
mc mb local/testdb

# Note: awsendpoint should use CLUSTER-IP of minio service in non-Minikube cluster.
cat <<EOF | kubectl -n $NAMESPACE exec -i $POD_NAME -- tee /home/dbadmin/minio.conf
awsauth = dbadmin:verticas3
awsendpoint = $(minikube ip):9000
awsenablehttps = 0
EOF

kubectl -n $NAMESPACE exec -i $POD_NAME -- /opt/vertica/bin/admintools \
  -t create_db \
  --hosts=$ALL_HOSTS \
  --communal-storage-location=s3://testdb \
  -x /home/dbadmin/minio.conf \
  --shard-count=3 \
  --depot-path=/home/dbadmin/local-data/depot \
  --depot-size=1G \
  --database testdb \
  --password vertica

kubectl -n $NAMESPACE exec -i $POD_NAME -- vsql -w vertica -c "select version()"
               version               
-------------------------------------
 Vertica Analytic Database v10.1.1-0
(1 row)
```

#### option 3.2: create Enterprise mode database

We can also create Enterprise mode database on Kubernetes, although that's not recommendation of Vertica.  

```BASH
RELEASE=vertica-release
SELECTOR=vertica.com/usage=server,app.kubernetes.io/name=vertica,app.kubernetes.io/instance=$RELEASE
NAMESPACE=default
ALL_HOSTS=$(kubectl get pods -n $NAMESPACE --selector=$SELECTOR -o=jsonpath='{range .items[*]}{.metadata.name}.{.spec.subdomain},{end}' | sed 's/.$//')
POD_NAME=$(kubectl get pods -n $NAMESPACE --selector=$SELECTOR -o jsonpath="{.items[0].metadata.name}")

kubectl -n $NAMESPACE exec -i $POD_NAME -- /opt/vertica/bin/admintools \
  -t create_db \
  --hosts=$ALL_HOSTS \
  --catalog_path=/home/dbadmin/local-data \
  --data_path=/home/dbadmin/local-data \
  --database testdb \
  --password vertica
```

### Notices

#### re-ip

Sometime we'd re-ip nodes of Vertica since ip of pod maybe change.  tool **restart_node** or **db_change_node_ip** can correct ip of nodes when database is UP, tool **re_ip** is helpful when database is down.

```BASH
kubectl exec vertica-release-defaultsubcluster-0 -- admintools -t restart_node --help
Usage: restart_node [options]

Options:
  -h, --help            show this help message and exit
  -s HOSTS, --hosts=HOSTS
                        comma-separated list of hosts to be restarted
  -d DB, --database=DB  Name of database whose node is to be restarted
  -p DBPASSWORD, --password=DBPASSWORD
                        Database password in single quotes
  --new-host-ips=NEWHOSTS
                        comma-separated list of new IPs for the hosts to be
                        restarted
  --timeout=NONINTERACTIVE_TIMEOUT
                        set a timeout (in seconds) to wait for actions to
                        complete ('never') will wait forever (implicitly sets
                        -i)
  -i, --noprompts       do not stop and wait for user input(default false).
                        Setting this implies a timeout of 20 min.
  -F, --force           force the node to start and auto recover if necessary
  --compat21            (deprecated) Use Vertica 2.1 method using node names
                        instead of hostnames

# db_change_node_ip does not require seeing the new IP in [Cluster] section, but db_replace_node does.
# db_replace_node cleans up the node being replaced like clean up data and catalog directories, it also creates relevant new directories on the new node. But db_change_node_ip does NOT, it only updates the IP address of the node everywhere.  
kubectl exec vertica-release-defaultsubcluster-0 -- admintools -t db_change_node_ip --help
Usage: db_change_node_ip [options]

Options:
  -h, --help            show this help message and exit
  -d DB, --database=DB  Name of the database
  -s HOSTS, --hosts=HOSTS
                        A comma separated list of names of hosts you wish to
                        re-ip
  -n NEWHOSTS, --new-host-ips=NEWHOSTS
                        A comma separated list of new IP addresses for the
                        hosts
  -p DBPASSWORD, --password=DBPASSWORD
                        Database password in single quotes
  --timeout=NONINTERACTIVE_TIMEOUT
                        set a timeout (in seconds) to wait for actions to
                        complete ('never') will wait forever (implicitly sets
                        -i)
  -i, --noprompts       do not stop and wait for user input(default false).
                        Setting this implies a timeout of 20 min.

# re_ip requires taking the entire cluster down, restart_nodes or db_change_node_ip require the database being UP.
kubectl exec vertica-release-defaultsubcluster-0 -- admintools -t re_ip --help
Usage: re_ip [options]

Replaces the IP addresses of hosts and databases in a cluster, or changes the
control messaging mode/addresses of a database.

Options:
  -h, --help            show this help message and exit
  -f MAPFILE, --file=MAPFILE
                        A text file with IP mapping information. If the -O
                        option is not used, the command replaces the IP
                        addresses of the hosts in the cluster and all
                        databases for those hosts. In this case, the format of
                        each line in MAPFILE is: [oldIPaddress newIPaddress]
                        or [oldIPaddress newIPaddress, newControlAddress,
                        newControlBroadcast]. If the former,
                        'newControlAddress' and 'newControlBroadcast' would
                        set to default values. Usage: $ admintools -t re_ip -f
                        <mapfile>
  -O, --db-only         Updates the control messaging addresses of a database.
                        Also used for error recovery (when Re-IP encounters
                        some certain errors, a mapfile is auto-generated).
                        Format of each line in MAPFILE: [NodeName
                        AssociatedNodeIPaddress, newControlAddress,
                        newControlBrodcast]. 'NodeName' and
                        'AssociatedNodeIPaddress' must be consistent with
                        admintools.conf. Usage: $ admintools -t re_ip -f
                        <mapfile> -O -d <db_name>
  -i, --noprompts       System does not prompt for the validation of the new
                        settings before performing the Re-IP. Prompting is on
                        by default.
  -T, --point-to-point  Sets the control messaging mode of a database to
                        point-to-point. Usage: $ admintools -t re_ip -d
                        <db_name> -T
  -U, --broadcast       Sets the control messaging mode of a database to
                        broadcast. Usage: $ admintools -t re_ip -d <db_name>
                        -U
  -d DB, --database=DB  Name of a database. Required with the following
                        options: -O, -T, -U.
```


## References

1. [Containerized Vertica](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/ContainerizedVertica.htm)
2. [Helm Chart Parameters of Vertica](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/HelmChartParams.htm)
3. [Vertica on Kubernetes Development Environment](https://verticaintegratorsguide.org/wiki/index.php?title=Vertica_on_Kubernetes_Development_Environment)
4. [Guide for Creating a Vertica Image, by yourself if you perfer](https://verticaintegratorsguide.org/wiki/index.php?title=Creating_a_Vertica_Image)
5. [What is Kubernetes (K8s)](https://www.bmc.com/blogs/what-is-kubernetes/)
6. [Kubernetes Documentation](https://kubernetes.io/docs/home/)
7. [Minikube Documentation](https://minikube.sigs.k8s.io/docs/)

