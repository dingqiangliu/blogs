# 瞬间完成故障节点接管的MPP数据库冗余节点方案
```text
                刘定强 2021.05.10 
```
[Click here for English version](spare-node-on-vertica-eon-mode_en.md) 

像Vertica这样开放架构的MPP分析数据库，由于采用工业标准化服务器，不可避免在某些时候会碰到内存、网卡、磁盘等硬件故障。Vertica自带高可用，通过节点间数据冗余来保证某些个节点硬件故障不会影响数据库继续使用。

但个别节点硬件故障还是会对系统有一些影响。首先，由于有的节点要同时负担原来两个节点的任务，查询用户会感觉到响应时间就会比正常时候长。另外，故障节点如果因为备件和维修流程等原因无法尽快修复，会让数据库较长时间处于亚健康状态，不利于保障系统长期可持续地高性能运行。

**Vertica企业模式**([Enterprise mode](https://www.vertica.com/docs/latest/HTML/Content/Authoring/ConceptsGuide/Components/ArchitectureOfTheVerticaCluster.htm))提供了可选的热备节点([Active Standby Nodes](https://www.vertica.com/docs/latest/HTML/Content/Authoring/AdministratorsGuide/ManageNodes/HotStandbyNodes.htm))功能。正常的时候热备节点在集群中不存放数据也不参与计算。根据预先制定的策略，当集群中有节点发生故障后较长时间没有被干预和修复，Vertica会自动用热备节点接管故障节点，以确保集群尽快恢复到设计的性能和可用状态。

企业模式的热备节点可以自动修复大规模集群的可用性，减少运维工作的人力负担。但由于Vertica企业模式计算和存储紧耦合，一般每个节点上持久化存储了10TB级别的数据，热备节点需要较长的时间复制完数据才能真正完成接管动作。

**Vertica计算存储分离模式**(**[Eon mode](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Eon/Architecture.htm)**的)的每个节点仍然只负责部分数据的处理和计算，节点的具体职责按订阅的分片([Shard](https://www.vertica.com/docs/latest/HTML/Content/Authoring/Eon/ShardsAndSubscriptions.htm))来划分。但由于计算和存储的职责分离，带来了负载横向隔离、弹性快速扩展等更多好处和灵活性。

![Eon Mode Shards and Subscriptions](https://www.vertica.com/docs/latest/HTML/Content/Resources/Images/Eon/eon-mode-Shard-Diagram-1.png)

我们通过在计算存储分离模式数据库的子集群中添加一个**冗余节点**，就可以轻松实现自动接管故障节点。这个冗余节点订阅所有分片，在正常时不会响应请求。一旦有某个节点发生故障，Vertica会立即自动选择冗余节点来承担故障节点的职责，确保数据库的可用资源和可靠性不变。

下面我们一个实验来验证计算存储分离模式下的Vertica冗余节点的自动、快速接管故障节点的能力。



## 1. 测试场景

我们的测试环境和场景如下：
- 3个节点的Vertica计算存储分离模式集群，有3个分片，ksafe=1，每个节点订阅两个不同分片以提供高可用性。 
- 先添加一个冗余节点，订阅所有分片，检查它是在正常情况下是否参与计算。
- 然后模拟硬件故障，停掉一个节点，验证冗余节点是否能立即接管故障节点。
- 最后模拟故障修复后，重启故障节点，验证修复的节点是否能立即恢复工作。

下面的查询结果展示了测试数据库中节点订阅分片的情况。

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

## 2. 添加冗余节点，冗余节点不参与工作

通过Vertica管理工具的[添加节点](https://www.vertica.com/docs/latest/HTML/Content/Authoring/AdministratorsGuide/ManageNodes/AddingNodes.htm)功能，添加一个冗余节点`v_testdb_node0004` ，让它订阅所有分片。

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

然后重复1000次连接数据库，检查系统表 [SESSION_SUBSCRIPTIONS](https://www.vertica.com/docs/latest/HTML/Content/Authoring/SQLReferenceManual/SystemTables/CATALOG/SESSION_SUBSCRIPTIONS.htm) 中显示的参与计算的节点集合，冗余节点`v_testdb_node0004`在正常情况下确实不参与计算。

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

## 3. 模拟节点硬件故障，冗余节点立即自动接管故障

用 Vertica 的管理工具杀掉节点 v_testdb_node0002(IP为172.17.0.4)，以模拟节点硬件故障。

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

然后立即重复1000次连接数据库，检查系统表 [SESSION_SUBSCRIPTIONS](https://www.vertica.com/docs/latest/HTML/Content/Authoring/SQLReferenceManual/SystemTables/CATALOG/SESSION_SUBSCRIPTIONS.htm) 中显示的参与计算的节点集合，冗余节点`v_testdb_node0004`已经自动接管了故障节点`v_testdb_node0002`的工作，计算所用的资源没有变化。

```BASH
$ for((i=0; i<1000; i++)) ; do \
    $VSQL -Aqt -c "select listagg(node_name) from (select distinct node_name from session_subscriptions where is_participating and shard_name <> 'replica' order by node_name) t" ; \
  done | sort -u
v_testdb_node0001,v_testdb_node0004,v_testdb_node0003
```

## 4. 恢复故障节点，一切恢复正常

用 Vertica 的管理工具重新启动节点`v_testdb_node0002`(IP为172.17.0.4)，模拟故障恢复。

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

然后立即重复1000次连接数据库，检查系统表 [SESSION_SUBSCRIPTIONS](https://www.vertica.com/docs/latest/HTML/Content/Authoring/SQLReferenceManual/SystemTables/CATALOG/SESSION_SUBSCRIPTIONS.htm) 中显示的参与计算的节点集合，故障节点`v_testdb_node0002`恢复后参与正常工作，冗余节点`v_testdb_node0004`和正常情况一样不再参与计算。

```BASH
$ for((i=0; i<1000; i++)) ; do \
    $VSQL -Aqt -c "select listagg(node_name) from (select distinct node_name from session_subscriptions where is_participating and shard_name <> 'replica' order by node_name) t" ; \
  done | sort -u
v_testdb_node0002,v_testdb_node0001,v_testdb_node0003
```
## 5. 结论

通过上面的测试可以得出结论，Vertica的计算存储分离模式确实带来了极高的灵活性和可管理性，与计算存储紧耦合的普通MPP数据库相比，额外少量的冗余节点就可以瞬间完成故障接管，能保障系统**可持续的性能和高可用性**。

