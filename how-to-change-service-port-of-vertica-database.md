# How to change service port of Vertica database

```text
                     DQ 2020.03.23
```

## backup database first

**NOTICE**: it's very import for you and your data.

## modify port of all node in database

```SH
dbadmin=> select name, clientport from vs_nodes;
select name, clientport from vs_nodes;
          name          | clientport
------------------------+------------
 v_testchgport_node0001 |       5433
(1 row)

dbadmin=> alter node v_testchgport_node0001 port 5435;
alter node v_testchgport_node0001 port 5435;
ALTER NODE
dbadmin=> select name, clientport from vs_nodes;
select name, clientport from vs_nodes;
          name          | clientport
------------------------+------------
 v_testchgport_node0001 |       5435
(1 row)
```

## stop database

```SH
admintools -t stop_db -d testchgport
```

## modify "port" property in /opt/vertica/config/admintools.conf

```SH
[dbadmin@v001 ~]$ sed -i -e 's/^\s*port\s*=.*$/port = 5435/g' /opt/vertica/config/admintools.conf
[dbadmin@v001 ~]$ grep -w port /opt/vertica/config/admintools.conf
port = 5435
```

## sync admintools.conf to all nodes

```SH
[dbadmin@v001 ~]$ admintools -t distribute_config_files
Initiating admintools.conf distribution...
Local admintools.conf sent to all nodes in the cluster.
```

## start database

```SH
admintools -t start_db -d testchgport
```
