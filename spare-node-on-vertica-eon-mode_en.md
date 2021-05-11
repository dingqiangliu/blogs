# A spare node takes over a failed node of Vertica cluster just under second
```text
              DQ 2021.05.10
```
[点击这里阅读中文版](spare-node-on-vertica-eon-mode_zh_CN.md) 

As an open-architecture MPP, Massively Parallel Processing, analytic database, Vertica prefers industry standard infrastructure, hardware such as memory, and network interface and disks failures sometimes happen. Vertica provide native high availability through data redundancy between nodes to ensure that database still works well when some node failed.

However, hardware failures will still have some impact on the system. First, as some node has to handle the tasks of failed node and itself at the same time, end users will feel longer query response time than normal. In addition, if the failed node cannot be back as soon as possible for maintenance procedures or other reasons, it will leave the database in a sub-healthy state for longer time then expectation, which is not good for stable performance and availability of the system.

**[Vertica Enterprise mode](https://www.vertica.com/docs/latest/HTML/Content/Authoring/ConceptsGuide/Components/ArchitectureOfTheVerticaCluster.htm)** provides optional [Active Standby Nodes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/AdministratorsGuide/ManageNodes/HotStandbyNodes.htm) feature. Standby node stays in the cluster and usually does not participate in processing and storing data. It will take over the failed node automatically according to a predefined strategy, or manually, to ensure performance and availability of database as expectation.

Active standby node can automatically heal large cluster and reduce the labor cost of operation. However, due to the tight coupling between computing and storage in Enterprise model and there is normally 10 TBs of data persisted on each node, it takes lots of  time for standby node to to replicate data and complete the takeover action finally.

In **[Vertica Eon mode](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Eon/Architecture.htm)**,  each node is still only responsible for processing part of the data , it depends on the [Shards](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Eon/ShardsAndSubscriptions.htm) it subscribes to. A lot of benefits, such as workloads isolation horizontally and elastic scalability, come from the separation of computing and storage.

![Eon Mode Shards and Subscriptions](https://www.vertica.com/docs/latest/HTML/Content/Resources/Images/Eon/eon-mode-Shard-Diagram-1.png)

A **spare node** in subclusters of Vertica Eon mode database can help automatically take over a failed node at blazing speed. A spare node subscribes to all shards and will not participate in response query usually. Once a node fails, the spare node will take over the failed node immediately to ensure enough resources and reliability of the database unchanged.

Now  let's verify how a spare node in a Vertica Eon mode database takes over the failed node with a experiment.



## 1. Testing scenarios

The testing environment and scenarios are as follows:
- A 3 nodes Vertica Eon mode database, with 3 shards and ksafe=1 for high availability.
- At first, add a spare node, subscribes to all shards, and check whether it participates in response query usually.
- Then, kill a node to simulate a hardware failure, and verify whether the spare node can immediately take over the failed node automatically.
- Finally, restart the failed node, and verify whether the failed node can immediately resume work without leveraging the spare node anymore.

The following query results show the shards, subscriptions of nodes in the test database.

```SQL
dbadmin=> select version();
               version               
-------------------------------------
 Vertica Analytic Database v10.1.1-0
(1 row)

dbadmin=> select shard_name, shard_type, is_replicated from shards;
 shard_name  | shard_type | is_replicated 
-------------+------------+---------------
 replica     | Replica    | t             
 segment0001 | Segment    | f             
 segment0002 | Segment    | f             
 segment0003 | Segment    | f             
(4 rows)

dbadmin=> select node_name, node_state, node_address, subcluster_name from nodes;
     node_name     | node_state | node_address |  subcluster_name   
-------------------+------------+--------------+--------------------
 v_testdb_node0001 | UP         | 172.17.0.3   | default_subcluster 
 v_testdb_node0002 | UP         | 172.17.0.4   | default_subcluster 
 v_testdb_node0003 | UP         | 172.17.0.5   | default_subcluster 
(3 rows)


dbadmin=> select node_name, shard_name, subscription_state, is_participating_primary from node_subscriptions order by node_name, shard_name;
     node_name     | shard_name  | subscription_state | is_participating_primary 
-------------------+-------------+--------------------+--------------------------
 v_testdb_node0001 | replica     | ACTIVE             | t
 v_testdb_node0001 | segment0001 | ACTIVE             | t
 v_testdb_node0001 | segment0003 | ACTIVE             | f
 v_testdb_node0002 | replica     | ACTIVE             | t
 v_testdb_node0002 | segment0001 | ACTIVE             | f
 v_testdb_node0002 | segment0002 | ACTIVE             | t
 v_testdb_node0003 | replica     | ACTIVE             | t
 v_testdb_node0003 | segment0002 | ACTIVE             | f
 v_testdb_node0003 | segment0003 | ACTIVE             | t
(9 rows)
```

## 2. Add a spare node and verify that the spare node will not participate in response query usually

Add a spare node `v_testdb_node0004` with the [Add Node](https://www.vertica.com/docs/latest/HTML/Content/Authoring/AdministratorsGuide/ManageNodes/AddingNodes.htm) function of Vertica's management tool,  and subscribes to all shards.

```SQL
dbadmin=> select node_name, node_state, node_address, subcluster_name from nodes;
     node_name     | node_state | node_address |  subcluster_name   
-------------------+------------+--------------+--------------------
 v_testdb_node0001 | UP         | 172.17.0.3   | default_subcluster 
 v_testdb_node0002 | UP         | 172.17.0.4   | default_subcluster 
 v_testdb_node0003 | UP         | 172.17.0.5   | default_subcluster 
 v_testdb_node0004 | UP         | 172.17.0.6   | default_subcluster 
(4 rows)

dbadmin=> select create_subscription('v_testdb_node0004','replica','PENDING',true);
 create_subscription 
---------------------
 CREATE SUBSCRIPTION
(1 row)

dbadmin=> select create_subscription('v_testdb_node0004','segment0001','PENDING',true);
 create_subscription 
---------------------
 CREATE SUBSCRIPTION
(1 row)

dbadmin=> select create_subscription('v_testdb_node0004','segment0002','PENDING',true);
 create_subscription 
---------------------
 CREATE SUBSCRIPTION

(1 row)
dbadmin=> select create_subscription('v_testdb_node0004','segment0003','PENDING',true);
 create_subscription 
---------------------
 CREATE SUBSCRIPTION
(1 row)

dbadmin=> select node_name, shard_name, subscription_state, is_participating_primary from node_subscriptions order by node_name, shard_name;
     node_name     | shard_name  | subscription_state | is_participating_primary 
-------------------+-------------+--------------------+--------------------------
 v_testdb_node0001 | replica     | ACTIVE             | t
 v_testdb_node0001 | segment0001 | ACTIVE             | t
 v_testdb_node0001 | segment0003 | ACTIVE             | f
 v_testdb_node0002 | replica     | ACTIVE             | t
 v_testdb_node0002 | segment0001 | ACTIVE             | f
 v_testdb_node0002 | segment0002 | ACTIVE             | t
 v_testdb_node0003 | replica     | ACTIVE             | t
 v_testdb_node0003 | segment0002 | ACTIVE             | f
 v_testdb_node0003 | segment0003 | ACTIVE             | t
 v_testdb_node0004 | replica     | ACTIVE             | t
 v_testdb_node0004 | segment0001 | ACTIVE             | t
 v_testdb_node0004 | segment0002 | ACTIVE             | t
 v_testdb_node0004 | segment0003 | ACTIVE             | t
(13 rows)
```

Then repeat 1000 times to connect to the database, the content of the system table [SESSION_SUBSCRIPTIONS](https://www.vertica.com/docs/latest/HTML/Content/Authoring/SQLReferenceManual/SystemTables/CATALOG/SESSION_SUBSCRIPTIONS.htm) shows that the spare node `v_testdb_node0004` never participates in response nodes list.

```BASH
$ $VSQL -c "select local_node_name()"
  local_node_name  
-------------------
 v_testdb_node0004

$ for((i=0; i<1000; i++)) ; do \
    $VSQL -Aqt -c "select listagg(node_name) from (select distinct node_name from session_subscriptions where is_participating and shard_name <> 'replica' order by node_name) t" ; \
  done | sort -u
v_testdb_node0002,v_testdb_node0001,v_testdb_node0003
```

## 3. Kill a node and verify the spare node can immediately take over the failed node automatically

To simulate a hardware failure,  kill the node `v_testdb_node0002`, with IP 172.17.0.4 here,  using management tool of Vertica.

```BASH
$ $VSQL -c "select local_node_name()"
  local_node_name  
-------------------
 v_testdb_node0004

$ admintools -t kill_node -s 172.17.0.4
Sending signal 'KILL' to ['172.17.0.4']
Successfully sent signal 'KILL' to hosts ['172.17.0.4'].
Details:
Host: 172.17.0.4 - Success - PID: 620 Signal KILL

Checking for processes to be down
All processes are down.
```

Then repeat 1000 times to connect to the database, the content of the system table [SESSION_SUBSCRIPTIONS](https://www.vertica.com/docs/latest/HTML/Content/Authoring/SQLReferenceManual/SystemTables/CATALOG/SESSION_SUBSCRIPTIONS.htm) shows that the spare node `v_testdb_node0004` automatically participates in response nodes list with role of failed node `v_testdb_node0002`.

```BASH
$ for((i=0; i<1000; i++)) ; do \
    $VSQL -Aqt -c "select listagg(node_name) from (select distinct node_name from session_subscriptions where is_participating and shard_name <> 'replica' order by node_name) t" ; \
  done | sort -u
v_testdb_node0001,v_testdb_node0004,v_testdb_node0003
```

## 4. Restart the failed node and verify the failed node can immediately resume work without leveraging the spare node anymore

To simulate failure recovery,  restart the node `v_testdb_node0002`, with IP 172.17.0.4 here,  using management tool of Vertica.

```BASH
$ $VSQL -c "select local_node_name()"
  local_node_name  
-------------------
 v_testdb_node0004

$ admintools -t restart_node -s 172.17.0.4 -d testdb
*** Restarting nodes for database testdb ***
	Restarting host [172.17.0.4] with catalog [v_testdb_node0002_catalog]
	Issuing multi-node restart
	Starting nodes: 
		v_testdb_node0002 (172.17.0.4)
	Starting Vertica on all nodes. Please wait, databases with a large catalog may take a while to initialize.
	Node Status: v_testdb_node0002: (DOWN) 
	Node Status: v_testdb_node0002: (UP) 
```

Then repeat 1000 times to connect to the database, the content of the system table [SESSION_SUBSCRIPTIONS](https://www.vertica.com/docs/latest/HTML/Content/Authoring/SQLReferenceManual/SystemTables/CATALOG/SESSION_SUBSCRIPTIONS.htm) shows that the recovered node `v_testdb_node0002` take back the role in response nodes list, there is no the spare node `v_testdb_node0004` in it any more.

```BASH
$ for((i=0; i<1000; i++)) ; do \
    $VSQL -Aqt -c "select listagg(node_name) from (select distinct node_name from session_subscriptions where is_participating and shard_name <> 'replica' order by node_name) t" ; \
  done | sort -u
v_testdb_node0002,v_testdb_node0001,v_testdb_node0003
```
## 5. Conclusion

Through the above experiment, it can be concluded that Vertica Eon mode does contribute extremely high flexibility and manageability. Compared with the ordinary MPP database with tightly coupled computing and storage, a spare node of Eon mode can automatically take over failure at blazing speed with low cost, which guarantee the system with **stable performance and high availability**.
