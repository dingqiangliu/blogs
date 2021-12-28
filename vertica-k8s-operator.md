# Setup Vertica Cluster on Kubernetes with Operator and CRD
```text
                            DQ 2021.11.15
```
Vertica is columnar MPP analytic database, separation computing from storage of its cloud-native Eon mode makes it elastic very well on Kubernetes platform, especially with its new Operator and CRD. 

![Vertica CRD and Operator](https://www.vertica.com/docs/latest/HTML/Content/Resources/Images/Containers/K8sClusterOperator.png)

## Install Minio on k8s optionally

Vertica Eon mode prefer S3-compatible object storage as its communal storage. Here we leverage Minio. You can skip this chapter If there is S3-compatible object storage service to use directly.

![Kubernetes Managed Object Storage with MinIO](https://blog.min.io/content/images/size/w2000/2021/04/MinIO-K8s_header.png)

### Step 1: Install Minio

Prerequisites:

```BASH
export CLUSTERNAME=vertica
# create k8s cluster. Here we test on minikube, optional parameters --image-* just for better experience in China mainland.
#minikube start -p ${CLUSTERNAME} --driver=docker --nodes=1 --extra-config=apiserver.service-node-port-range=1-65535 # --image-mirror-country='cn' --image-repository='registry.cn-hangzhou.aliyuncs.com/google_containers' 

kubectl get pod kube-controller-manager-${CLUSTERNAME} -n kube-system -o yaml | grep 'cluster-signing-cert-file\|cluster-signing-key-file' || echo 'The Operator cannot complete initialization if the Kubernetes cluster is not configured to respond to a generated CSR. Certain Kubernetes providers do not specify these configuration values by default.'
```



#### Option 1.1: Install Online

Plugin minio can handle almost everything about Minio, you only need install it explicitly. Plugin view-secret is optional, it can help us get forgotten secrets.

```BASH
# install the MinIO Operator and Plugin
kubectl krew install minio view-secret
kubectl minio version || echo 'Install the MinIO Kubernetes Operator failed!'
```

#### Option 1.2: Install Offline

Maybe Internet is not available to your environment , you can download related docker images,  operator and kubectl plugins when Internet is accessible, and install them latter.

```BASH
# download docker image
docker login
docker pull minio/operator:v4.3.5 ; docker image save minio/operator:v4.3.5 | gzip > minio-operator-v4.3.5.tgz
docker pull minio/console:v0.12.3 ; docker image save minio/console:v0.12.3 | gzip > minio-console-v0.12.3.tgz
docker pull minio/minio:RELEASE.2021-11-09T03-21-45Z ; docker image save minio/minio:RELEASE.2021-11-09T03-21-45Z | gzip > minio-minio-RELEASE.2021-11-09T03-21-45Z.tgz

# download the MinIO Operator and Plugin
wget https://github.com/minio/operator/releases/download/v4.2.7/kubectl-minio_4.2.7_linux_amd64 -O kubectl-minio
wget https://github.com/elsesiy/kubectl-view-secret/releases/download/v0.8.1/kubectl-view-secret_v0.8.1_linux_amd64.tar.gz -O - | tar -xz kubectl-view-secret
chmod +x kubectl-*

###############################

# load images to local registry or docker of every node
gunzip -c minio-operator-v4.3.5.tgz | minikube ssh --native-ssh=false docker image load
gunzip -c minio-console-v0.12.3.tgz | minikube ssh --native-ssh=false docker image load
gunzip -c minio-minio-RELEASE.2021-11-09T03-21-45Z.tgz | minikube ssh --native-ssh=false docker image load

# install the MinIO Operator and Plugin
cp kubectl-* /usr/local/bin/
kubectl minio version || echo 'Install the MinIO Kubernetes Operator failed!'
```

### Step 2: Create Minio Tenant

Create tenant of Minio is quit easy with kubectl plugin and operator.

```BASH
# initialize the MinIO Kubernetes Operator
kubectl minio init
kubectl get all --namespace minio-operator || echo 'initialize Minio Operator failed!'

# create StorageClass
kubectl apply -f <(cat <<-'EOF'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
   name: minio-local-storage
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
EOF
)
kubectl get sc minio-local-storage || echo 'create SC failed!'

# create StorageClass
for i in $(seq 1  4) ; do
  minikube ssh "sudo mkdir -p /data/minio/disk${i}; sudo chmod a+rwx -R /data/minio/disk${i}"

  kubectl apply -f <(cat <<-EOF
apiVersion: v1
kind: PersistentVolume
metadata:
  name: minio-local-storage-pv${i}
spec:
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  storageClassName: minio-local-storage
  capacity:
    storage: 10Gi
  hostPath:
    path: /data/minio/disk${i}
EOF
)
done
kubectl get pv | grep minio-local-storage-pv || echo 'create PV failed!'

# create tenant
#kubectl create namespace default
kubectl minio tenant create verticas3 --servers 1 --volumes 4 --capacity 1G --storage-class minio-local-storage --namespace default

kubectl minio tenant list | grep verticas3
kubectl get svc | grep 'minio\|verticas3'

# do not expose service as default
# kubectl patch service minio --type=merge -p '{"spec": {"type": "ClusterIP", "externalIPs":[]}}'

# expose service to LoadBalancer, replace "$(minikube ip)" with your real address of LoadBalancer 
kubectl patch service minio --type=merge -p '{"spec": {"type": "LoadBalancer", "externalIPs":["'$(minikube ip)'"], "ports": [{"name": "https-minio", "protocol": "TCP", "port": 443, "nodePort": 9000, "targetPort": 9000 }]}}'
```

### Step 3: Test Minio

You can access your S3 storage through service name in k8s cluster, or through loadbalancer in world out of k8s.

```BASH
# replace "$(minikube ip)" with your real address of LoadBalancer
alias MC="mc --insecure"
MC config host add verticas3-admin https://$(minikube ip):9000 $(kubectl view-secret verticas3-user-1 -a | awk -F '=' '{print $2}') # accessKey and secretKey come from output of command "kubectl minio tenant create ...", looks like admin 98b4fca9-a036-4aa4-8460-f2883949a65f
MC admin info verticas3-admin/ || echo 'mc config failed'
MC admin user add verticas3-admin/ dbadmin verticas3
MC admin policy set verticas3-admin/ readwrite user=dbadmin
MC config host add verticas3 https://$(minikube ip):9000 dbadmin verticas3

# create bucket for database and files
MC mb verticas3/test

# generate data for test
seq 1 1000 | MC pipe verticas3/test/test.csv
MC cat verticas3/test/test.csv | wc -l
# 1000
```

## Setup Vertica on k8s

### Step 1: Install Vertica

####   Option 1.1: Install Inline

Operator can handle almost everything about Vertica, you only need install it explicitly.

```BASH
# Installing cert-manager
kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.5.3/cert-manager.yaml
kubectl get pods --namespace cert-manager

# Installing the VerticaDB Operator and Admission Controller
helm repo add vertica-charts https://vertica.github.io/charts
helm repo update
helm install vdb-op vertica-charts/verticadb-operator
```

####   Option 1.2: Install Offline

Maybe Internet is not available to your environment , you can download related docker images and Helm Chart when Internet is accessible, and install them latter.

```BASH
# download docker image
docker login; 
docker pull quay.io/jetstack/cert-manager-controller:v1.5.3 ; docker image save quay.io/jetstack/cert-manager-controller:v1.5.3 | gzip > cert-manager-controller-v1.5.3.tgz
docker pull quay.io/jetstack/cert-manager-cainjector:v1.5.3 ; docker image save quay.io/jetstack/cert-manager-cainjector:v1.5.3 | gzip > cert-manager-cainjector-v1.5.3.tgz
docker pull quay.io/jetstack/cert-manager-webhook:v1.5.3 ; docker image save quay.io/jetstack/cert-manager-webhook:v1.5.3 | gzip > cert-manager-webhook-v1.5.3.tgz
docker pull gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0 ; docker image save gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0 | gzip > kube-rbac-proxy-v0.8.0.tgz
docker pull vertica/verticadb-operator:1.1.0 ; docker image save vertica/verticadb-operator:1.1.0 | gzip > verticadb-operator-1.1.0-img.tgz
docker pull vertica/vertica-k8s:11.0.1-0 ; docker image save vertica/vertica-k8s:11.0.1-0 | gzip > vertica-k8s-11.0.1-0.tgz

# Installing cert-manager
wget https://github.com/jetstack/cert-manager/releases/download/v1.5.3/cert-manager.yaml

# download Helm Chart
#wget https://github.com/vertica/vertica-kubernetes/releases/download/v1.1.0/verticadbs.vertica.com-crd.yaml
wget https://github.com/vertica/vertica-kubernetes/releases/download/v1.1.0/verticadb-operator-1.1.0.tgz
wget https://github.com/vertica/vertica-kubernetes/releases/download/v1.1.0/verticadb-webhook-1.0.1.tgz

##########################################

# load image to local registry or docker of every node
gunzip -c cert-manager-controller-v1.5.3.tgz | minikube ssh --native-ssh=false docker image load
gunzip -c cert-manager-cainjector-v1.5.3.tgz | minikube ssh --native-ssh=false docker image load
gunzip -c cert-manager-webhook-v1.5.3.tgz | minikube ssh --native-ssh=false docker image load
gunzip -c kube-rbac-proxy-v0.8.0.tgz | minikube ssh --native-ssh=false docker image load
gunzip -c verticadb-operator-1.1.0-img.tgz | minikube ssh --native-ssh=false docker image load
gunzip -c vertica-k8s-11.0.1-0.tgz | minikube ssh --native-ssh=false docker image load

# Installing cert-manager
kubectl apply -f cert-manager.yaml
kubectl get pods --namespace cert-manager

# Installing the VerticaDB Operator and Admission Controller
helm install vdb-op verticadb-operator-1.1.0.tgz
```

### Step 2: Create Vertica Database

Create or patch Vertica database is simple and straightforward for person familiar with k8s , just leverage kubectl and CRD.

```BASH
# optional step for nodes number more than 3
kubectl create secret generic vertica-license --from-file=license.dat=vlicense.dat

# create a secret named su-passwd to store your superuser password
kubectl create secret generic su-passwd --from-literal=password=vertica

# store S3-compatible communal access and secret key credentials in a secret named s3-creds
kubectl create secret generic s3-creds --from-literal=accesskey=dbadmin --from-literal=secretkey=verticas3

# configures a certificate authority (CA) bundle that authenticates the S3-compatible connections
openssl s_client -showcerts -connect $(minikube ip):9000 <<<'n' | openssl x509 -out root-cert.pem
kubectl create secret generic aws-cert --from-file=root-cert.pem

# create Vertica cluster
kubectl apply -f <(cat <<-EOF
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: testdb
spec:
  image: vertica/vertica-k8s:11.0.1-0
  imagePullPolicy: IfNotPresent
  dbName: testdb
  initPolicy: Create
  licenseSecret: vertica-license
  superuserPasswordSecret: su-passwd
  certSecrets:
    - name: aws-cert
  local:
    requestSize: 1Gi
  shardCount: 12
  communal:
    credentialSecret: s3-creds
    endpoint: https://minio.default.svc.cluster.local
    caFile: /certs/aws-cert/root-cert.pem
    path: s3://test/testdb
  subclusters:
  - isPrimary: true
    name: primarysubcluster
    size: 3
    serviceType: ClusterIP
EOF
)

kubectl describe verticadbs.vertica.com testdb

# do not expose service as default
# kubectl patch verticadbs.vertica.com testdb  --type=merge -p '{"spec": {"subclusters": [{"name": "primarysubcluster", "serviceType": "ClusterIP", "externalIPs":[]}]}}'

# expose service to LoadBalancer, replace "$(minikube ip)" with your real address of LoadBalancer 
kubectl patch verticadbs.vertica.com testdb  --type=merge -p '{"spec": {"subclusters": [{"name": "primarysubcluster", "serviceType": "LoadBalancer", "externalIPs":["'$(minikube ip)'"], "nodePort": 5433}]}}'

kubectl get svc testdb-primarysubcluster -o jsonpath="{.*.externalIPs[*]}"
# 192.168.76.2

# connect to database, replace "$(minikube ip)" with your real address of LoadBalancer
alias VSQL="vsql -h $(minikube ip) -U dbadmin -w vertica"
for in in $(seq 1 9) ; do  VSQL -Aqt -c "select local_node_name()" ; done | sort | uniq -c
# 2 v_testdb_node0001
# 3 v_testdb_node0002
# 4 v_testdb_node0003
```

## Scale Vertica horizontally

### Add a secondary subcluster

```BASH
# add another subcluster and expose service to LoadBalancer, replace "$(minikube ip)" with your real address of LoadBalancer 
kubectl patch verticadbs.vertica.com testdb  --type=merge -p '{"spec": {"subclusters": [{"name": "primarysubcluster", "isPrimary": true, "size": 3, "serviceType": "LoadBalancer", "externalIPs":["'$(minikube ip)'"], "nodePort": 5433}, {"name": "secondarysubcluster", "isPrimary": false, "size": 3, "serviceType": "LoadBalancer", "externalIPs":["'$(minikube ip)'"], "nodePort": 5435}]}}'

# connect to database, replace "$(minikube ip)" with your real address of LoadBalancer
alias VSQL2="vsql -h $(minikube ip) -U dbadmin -w vertica -p 5435"
for in in $(seq 1 9) ; do  VSQL2 -Aqt -c "select local_node_name()" ; done | sort | uniq -c
# 4 v_testdb_node0004
# 3 v_testdb_node0005
# 2 v_testdb_node0006
```

### Add more nodes to the secondary subcluster 

```BASH
# connect to database, replace "$(minikube ip)" with your real address of LoadBalancer
alias VSQL2="vsql -h $(minikube ip) -U dbadmin -w vertica -p 5435"

# there are only 3 nodes participating
VSQL2 -Aqtc "select listagg(distinct node_name) from(select node_name from session_subscriptions where is_participating order by 1) t"
# v_testdb_node0004,v_testdb_node0005,v_testdb_node0006

# add one more node to the secondary subcluster 
kubectl patch verticadbs.vertica.com testdb  --type=merge -p '{"spec": {"subclusters": [{"name": "primarysubcluster", "isPrimary": true, "size": 3, "serviceType": "LoadBalancer", "externalIPs":["'$(minikube ip)'"], "nodePort": 5433}, {"name": "secondarysubcluster", "isPrimary": false, "size": 4, "serviceType": "LoadBalancer", "externalIPs":["'$(minikube ip)'"], "nodePort": 5435}]}}'

# there are 4 nodes participating at now
VSQL2 -Aqtc "select listagg(distinct node_name) from(select node_name from session_subscriptions where is_participating order by 1) t"
# v_testdb_node0004,v_testdb_node0005,v_testdb_node0006,v_testdb_node0007
```

### Remove nodes from the secondary subcluster 

```BASH
# connect to database, replace "$(minikube ip)" with your real address of LoadBalancer
alias VSQL2="vsql -h $(minikube ip) -U dbadmin -w vertica -p 5435"

# there are 4 nodes participating at now
VSQL2 -Aqtc "select listagg(distinct node_name) from(select node_name from session_subscriptions where is_participating order by 1) t"
# v_testdb_node0004,v_testdb_node0005,v_testdb_node0006,v_testdb_node0007

# remove one more node from the secondary subcluster 
kubectl patch verticadbs.vertica.com testdb  --type=merge -p '{"spec": {"subclusters": [{"name": "primarysubcluster", "isPrimary": true, "size": 3, "serviceType": "LoadBalancer", "externalIPs":["'$(minikube ip)'"], "nodePort": 5433}, {"name": "secondarysubcluster", "isPrimary": false, "size": 3, "serviceType": "LoadBalancer", "externalIPs":["'$(minikube ip)'"], "nodePort": 5435}]}}'

# there are 3 nodes participating at now
VSQL2 -Aqtc "select listagg(distinct node_name) from(select node_name from session_subscriptions where is_participating order by 1) t"
# v_testdb_node0004,v_testdb_node0005,v_testdb_node0006
```

### Remove the secondary subcluster

```BASH
# connect to database, replace "$(minikube ip)" with your real address of LoadBalancer
alias VSQL="vsql -h $(minikube ip) -U dbadmin -w vertica"

# there are 2 subclusters at now
VSQL -Aqtc "select listagg(distinct subcluster_name) from(select subcluster_name from subclusters order by 1) t"
# primarysubcluster,secondarysubcluster

# remove the secondary subcluster 
kubectl patch verticadbs.vertica.com testdb  --type=merge -p '{"spec": {"subclusters": [{"name": "primarysubcluster", "isPrimary": true, "size": 3, "serviceType": "LoadBalancer", "externalIPs":["'$(minikube ip)'"], "nodePort": 5433}]}}'

# there is one 1 subcluster at now
VSQL -Aqtc "select listagg(distinct subcluster_name) from(select subcluster_name from subclusters order by 1) t"
# primarysubcluster
```
## Terminate and Revive Vertica
### Terminate Database 

```BASH
# sync database to communal storage
VSQL -Aqtc "select sync_catalog()"

# terminate Vertica cluster
kubectl delete verticadbs.vertica.com testdb && for pvc in $(kubectl get persistentvolumeclaims --selector=vertica.com/database=testdb -o jsonpath="{.items[*].metadata.name}"); do kubectl delete persistentvolumeclaims ${pvc} ; done
```

### Revive Database

Note:  properties "**initPolicy**" is "***Revive***" here. Please replace "$(minikube ip)" with your real address of LoadBalancer.

```BASH
kubectl apply -f <(cat <<-EOF
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: testdb
spec:
  image: vertica/vertica-k8s:11.0.1-0
  imagePullPolicy: IfNotPresent
  dbName: testdb
  initPolicy: Revive
  licenseSecret: vertica-license
  superuserPasswordSecret: su-passwd
  certSecrets:
    - name: aws-cert
  local:
    requestSize: 1Gi
  shardCount: 12
  communal:
    credentialSecret: s3-creds
    endpoint: https://minio.default.svc.cluster.local
    caFile: /certs/aws-cert/root-cert.pem
    path: s3://test/testdb
  subclusters:
  - isPrimary: true
    name: primarysubcluster
    size: 3
    serviceType: LoadBalancer
    externalIPs:
    - $(minikube ip)
    nodePort: 5433
EOF
)
```

## Upgrade Database

Upgrading a Vertica database can be achieved by just a single kubectl patch command.

```BASH
# connect to database, replace "$(minikube ip)" with your real address of LoadBalancer
alias VSQL="vsql -h $(minikube ip) -U dbadmin -w vertica"

VSQL -Aqtc "select version()"
# Vertica Analytic Database v11.0.1-0

kubectl patch verticadbs.vertica.com testdb --type=merge -p '{"spec": {"image": "vertica/vertica-k8s:11.0.1-2"}}'

VSQL -Aqtc "select version()"
# Vertica Analytic Database v11.0.1-2
```

## Play with Your Database

You can connect your database through subcluster service name in k8s cluster, or through loadbalancer in world out of k8s.

```BASH
# connect to database, replace "$(minikube ip)" with your real address of LoadBalancer
alias VSQL="vsql -h $(minikube ip) -U dbadmin -w vertica"

VSQL -c "create table test(id int)"
VSQL -c "copy test from 's3://test/test.csv'"
VSQL -Aqtc "select count(*) from test"
# 1000

VSQL -c "export to parquet(directory='s3://test/test.parquet') as select * from test"

VSQL -c "create external table test_ext(id int) as copy from 's3://test/test.parquet/*' parquet"
VSQL -Aqtc "select count(*) from test_ext"
# 1000
```

Have fun!

### Notices

#### Troubleshooting

Here are some commands for troubleshooting:

```BASH
kubectl get verticadbs.vertica.com testdb
kubectl describe verticadbs.vertica.com testdb
kubectl get svc -o wide
kubectl get pods -o wide 
kubectl get events -w
kubectl logs -f $RPOD_NAME
kubectl exec $POD_NAME -it -- /bin/bash
```

## References

1. [Containerized Vertica](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/ContainerizedVertica.htm)
2. [Helm Chart Parameters of Vertica](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/Kubernetes/HelmChartParams.htm)
3. [Vertica on Kubernetes Development Environment](https://verticaintegratorsguide.org/wiki/index.php?title=Vertica_on_Kubernetes_Development_Environment)
4. [Guide for Creating a Vertica Image, by yourself if you perfer](https://verticaintegratorsguide.org/wiki/index.php?title=Creating_a_Vertica_Image)
5. [What is Kubernetes (K8s)](https://www.bmc.com/blogs/what-is-kubernetes/)
6. [Kubernetes Documentation](https://kubernetes.io/docs/home/)
7. [Minikube Documentation](https://minikube.sigs.k8s.io/docs/)

