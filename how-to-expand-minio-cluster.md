# How to expand Minio cluster

```text
                     DQ 2020.03.05
```

Minio is a high performance S3 storage with enough simplicity, it saves each object to directories in one of disksets. To expand a Minio cluster, the easy way is just adding or removing zones.

To rebalance storage before remove zone, or maybe after add zone, just **move** object between disksets. Here **move** means copy directories of objects to temp localtion, rename and delete on file system, but with patience.

Although there is no public official tool for this at now, you can do it with commands rsync/cp/mv/rm according the diskset map.

**Note**: actually there is no way to stop new objects put to removing zone on a live cluster at now, so a maintain window required to evict zone.

Here are steps to demo how to achieve it.

## setup cluster with 1 zone

 ```BASH
[dbadmin ~]$ minio --version
minio version RELEASE.2020-02-27T00-23-05Z

[dbadmin ~]$ export NODE_LIST="192.168.33.105"

[dbadmin ~]$ cls_run -p mkdir -p /home/minio1{1..4}/
[dbadmin ~]$ cls_run -p ls -l /home/minio1{1..4}/
[192.168.33.105] /home/minio11/:
[192.168.33.105] total 0
[192.168.33.105]
[192.168.33.105] /home/minio12/:
[192.168.33.105] total 0
[192.168.33.105]
[192.168.33.105] /home/minio13/:
[192.168.33.105] total 0
[192.168.33.105]
[192.168.33.105] /home/minio14/:
[192.168.33.105] total 0

[dbadmin ~]$ cls_run -p sudo -u dbadmin "sed -i -e 's/^\s*MINIO_VOLUMES\s*=.*$/MINIO_VOLUMES=\"http:\/\/192.168.33.105:9000\/home\/minio1{1...4}\"/g' /opt/vertica/config/minio.conf"
[dbadmin ~]$ cls_run -p sudo -u dbadmin egrep '^\s*MINIO_VOLUMES\s*=' /opt/vertica/config/minio.conf
[192.168.33.105] MINIO_VOLUMES="http://192.168.33.105:9000/home/minio1{1...4}"

[dbadmin ~]$ cls_run -p sudo systemctl start minio
[dbadmin ~]$ mc admin info mys3
●  192.168.33.105:9000
   Uptime: 18 seconds
   Version: 2020-02-27T00:23:05Z
   Network: 1/1 OK
   Drives: 4/4 OK

4 drives online, 0 drives offline

[dbadmin ~]$ mc mb mys3/test
[dbadmin ~]$ seq 1 1000 | mc pipe mys3/test/file1.txt
[dbadmin ~]$ mc cat mys3/test/file1.txt | wc -l
1000
[dbadmin ~]$ seq 1 1000 | mc pipe mys3/test/file2.txt
[dbadmin ~]$ mc cat mys3/test/file2.txt | wc -l
1000
```
  
## add 1 zone to cluster

 ```BASH
# add zone

[dbadmin ~]$ ssh 192.168.33.106 mkdir -p /home/minio1{1..4}/

[dbadmin ~]$ export NODE_LIST="192.168.33.105 192.168.33.106"

[dbadmin ~]$ cls_run -p sudo -u dbadmin "sed -i -e 's/^\s*MINIO_VOLUMES\s*=.*$/MINIO_VOLUMES=\"http:\/\/192.168.33.105:9000\/home\/minio1{1...4} http:\/\/192.168.33.106:9000\/home\/minio1{1...4}\"/g' /opt/vertica/config/minio.conf"
[dbadmin ~]$ cls_run -p sudo -u dbadmin egrep '^\s*MINIO_VOLUMES\s*=' /opt/vertica/config/minio.conf
[192.168.33.105] MINIO_VOLUMES="http://192.168.33.105:9000/home/minio1{1...4} http://192.168.33.106:9000/home/minio1{1...4}"
[192.168.33.106] MINIO_VOLUMES="http://192.168.33.105:9000/home/minio1{1...4} http://192.168.33.106:9000/home/minio1{1...4}"

# restart cluster
[dbadmin ~]$ cls_run -p sudo systemctl restart minio
[dbadmin ~]$ mc admin info mys3
●  192.168.33.106:9000
   Uptime: 6 seconds
   Version: 2020-02-27T00:23:05Z
   Network: 2/2 OK
   Drives: 4/4 OK
●  192.168.33.105:9000
   Uptime: 6 seconds
   Version: 2020-02-27T00:23:05Z
   Network: 2/2 OK
   Drives: 4/4 OK

8 drives online, 0 drives offline

# check files
[dbadmin ~]$ mc cat mys3/test/file1.txt | wc -l
1000
[dbadmin ~]$ mc cat mys3/test/file2.txt | wc -l
1000

[dbadmin ~]$ cls_run -p du -hs "/home/minio1{1..4}/test/"
[192.168.33.105] 28K    /home/minio11/test/
[192.168.33.105] 28K    /home/minio12/test/
[192.168.33.105] 28K    /home/minio13/test/
[192.168.33.105] 28K    /home/minio14/test/
[192.168.33.106] 4.0K    /home/minio11/test/
[192.168.33.106] 4.0K    /home/minio12/test/
[192.168.33.106] 4.0K    /home/minio13/test/
[192.168.33.106] 4.0K    /home/minio14/test/

# rebalance storage manually
[dbadmin ~]$ for i in {1..4} ; do scp -r /home/minio1${i}/test/file2.txt  192.168.33.106:/home/minio1${i}/test/ & done; wait
[1]   Done                    scp -r /home/minio1${i}/test/file2.txt 192.168.33.106:/home/minio1${i}/test/
[2]   Done                    scp -r /home/minio1${i}/test/file2.txt 192.168.33.106:/home/minio1${i}/test/
[3]-  Done                    scp -r /home/minio1${i}/test/file2.txt 192.168.33.106:/home/minio1${i}/test/
[4]+  Done                    scp -r /home/minio1${i}/test/file2.txt 192.168.33.106:/home/minio1${i}/test/

[dbadmin ~]$ cls_run -p du -hs "/home/minio1{1..4}/test/file2.txt"
[192.168.33.105] 12K    /home/minio11/test/file2.txt
[192.168.33.105] 12K    /home/minio12/test/file2.txt
[192.168.33.105] 12K    /home/minio13/test/file2.txt
[192.168.33.105] 12K    /home/minio14/test/file2.txt
[192.168.33.106] 12K    /home/minio11/test/file2.txt
[192.168.33.106] 12K    /home/minio12/test/file2.txt
[192.168.33.106] 12K    /home/minio13/test/file2.txt
[192.168.33.106] 12K    /home/minio14/test/file2.txt

# check files again
[dbadmin ~]$ mc cat mys3/test/file1.txt | wc -l
1000
[dbadmin ~]$ mc cat mys3/test/file2.txt | wc -l
1000

[dbadmin ~]$ for i in {1..4} ; do rm -rf /home/minio1${i}/test/file2.txt & done; wait
[1]   Done                    rm -rf /home/minio1${i}/test/file2.txt
[2]   Done                    rm -rf /home/minio1${i}/test/file2.txt
[3]-  Done                    rm -rf /home/minio1${i}/test/file2.txt
[4]+  Done                    rm -rf /home/minio1${i}/test/file2.txt

# check files once again
[dbadmin ~]$ mc cat mys3/test/file1.txt | wc -l
1000
[dbadmin ~]$ mc cat mys3/test/file2.txt | wc -l
1000

[dbadmin ~]$ cls_run -p du -hs "/home/minio1{1..4}/test/*"
[192.168.33.105] 12K    /home/minio11/test/file1.txt
[192.168.33.105] 12K    /home/minio12/test/file1.txt
[192.168.33.105] 12K    /home/minio13/test/file1.txt
[192.168.33.105] 12K    /home/minio14/test/file1.txt
[192.168.33.106] 12K    /home/minio11/test/file2.txt
[192.168.33.106] 12K    /home/minio12/test/file2.txt
[192.168.33.106] 12K    /home/minio13/test/file2.txt
[192.168.33.106] 12K    /home/minio14/test/file2.txt
 ```

## remove 1 zone from cluster

 ```BASH

# check files
[dbadmin ~]$ mc cat mys3/test/file1.txt | wc -l
1000
[dbadmin ~]$ mc cat mys3/test/file2.txt | wc -l
1000

[dbadmin ~]$ cls_run -p du -hs "/home/minio1{1..4}/test/*"
[192.168.33.105] 12K    /home/minio11/test/file1.txt
[192.168.33.105] 12K    /home/minio12/test/file1.txt
[192.168.33.105] 12K    /home/minio13/test/file1.txt
[192.168.33.105] 12K    /home/minio14/test/file1.txt
[192.168.33.106] 12K    /home/minio11/test/file2.txt
[192.168.33.106] 12K    /home/minio12/test/file2.txt
[192.168.33.106] 12K    /home/minio13/test/file2.txt
[192.168.33.106] 12K    /home/minio14/test/file2.txt

# rebalance storage manually
[dbadmin ~]$ for i in {1..4} ; do scp -r 192.168.33.106:/home/minio1${i}/test/file2.txt  /home/minio1${i}/test/file2.txt.tmp & done; wait
1]   Done                    scp -r 192.168.33.106:/home/minio1${i}/test/file2.txt /home/minio1${i}/test/file2.txt.tmp
[3]-  Done                    scp -r 192.168.33.106:/home/minio1${i}/test/file2.txt /home/minio1${i}/test/file2.txt.tmp
[2]-  Done                    scp -r 192.168.33.106:/home/minio1${i}/test/file2.txt /home/minio1${i}/test/file2.txt.tmp
[4]+  Done                    scp -r 192.168.33.106:/home/minio1${i}/test/file2.txt /home/minio1${i}/test/file2.txt.tmp

[dbadmin ~]$ for i in {1..4} ; do mv /home/minio1${i}/test/file2.txt.tmp /home/minio1${i}/test/file2.txt & done; wait
[1]   Done                    mv /home/minio1${i}/test/file2.txt.tmp /home/minio1${i}/test/file2.txt
[4]+  Done                    mv /home/minio1${i}/test/file2.txt.tmp /home/minio1${i}/test/file2.txt
[2]-  Done                    mv /home/minio1${i}/test/file2.txt.tmp /home/minio1${i}/test/file2.txt
[3]+  Done                    mv /home/minio1${i}/test/file2.txt.tmp /home/minio1${i}/test/file2.txt

# check files again
[dbadmin ~]$ mc cat mys3/test/file1.txt | wc -l
1000
[dbadmin ~]$ mc cat mys3/test/file2.txt | wc -l
1000

[dbadmin ~]$ for i in {1..4} ; do ssh 192.168.33.106 rm -rf /home/minio1${i}/test/file2.txt & done; wait
[1]   Done                    ssh 192.168.33.106 rm -rf /home/minio1${i}/test/file2.txt
[3]-  Done                    ssh 192.168.33.106 rm -rf /home/minio1${i}/test/file2.txt
[2]-  Done                    ssh 192.168.33.106 rm -rf /home/minio1${i}/test/file2.txt
[4]+  Done                    ssh 192.168.33.106 rm -rf /home/minio1${i}/test/file2.txt

[dbadmin ~]$ mc cat mys3/test/file1.txt | wc -l
1000
[dbadmin ~]$ mc cat mys3/test/file2.txt | wc -l
1000

[dbadmin ~]$ cls_run -p du -hs "/home/minio1{1..4}/test/"
[192.168.33.105] 28K    /home/minio11/test/
[192.168.33.105] 28K    /home/minio12/test/
[192.168.33.105] 28K    /home/minio13/test/
[192.168.33.105] 28K    /home/minio14/test/
[192.168.33.106] 4.0K    /home/minio11/test/
[192.168.33.106] 4.0K    /home/minio12/test/
[192.168.33.106] 4.0K    /home/minio13/test/
[192.168.33.106] 4.0K    /home/minio14/test/

# remove zone
[dbadmin ~]$ export NODE_LIST="192.168.33.105"

[dbadmin ~]$ cls_run -p sudo -u dbadmin "sed -i -e 's/^\s*MINIO_VOLUMES\s*=.*$/MINIO_VOLUMES=\"http:\/\/192.168.33.105:9000\/home\/minio1{1...4}\"/g' /opt/vertica/config/minio.conf"
[dbadmin ~]$ cls_run -p sudo -u dbadmin egrep '^\s*MINIO_VOLUMES\s*=' /opt/vertica/config/minio.conf
[192.168.33.105] MINIO_VOLUMES="http://192.168.33.105:9000/home/minio1{1...4}"

[dbadmin ~]$ cls_run -p sudo systemctl restart minio
[dbadmin ~]$ mc admin info mys3
●  192.168.33.105:9000
   Uptime: 18 seconds
   Version: 2020-02-27T00:23:05Z
   Network: 1/1 OK
   Drives: 4/4 OK

4 drives online, 0 drives offline

# check files
[dbadmin ~]$ mc cat mys3/test/file1.txt | wc -l
1000
[dbadmin ~]$ mc cat mys3/test/file2.txt | wc -l
1000
 ```
