# A dedicated node in Vertica for some special tasks
```text
              DQ 2021.05.27
```
Sometimes we need a special dedicated node in Vertica database for special tasks, such as high concurrent & heavy data loading workload from client, or just query catalog. We don't want those tasks impact others, and we also hope this node will not involve  in other tasks including native load balance.

Actually Vertica support a special node type, [EXECUTE](https://www.vertica.com/docs/latest/HTML/Content/Authoring/SQLReferenceManual/Statements/ALTERNODE.htm?zoom_highlight=EXECUTE), which can meet this special requirement.

Now  let's verify how a EXECUTE type node of Vertica can work as a dedicated node with a experiment.

## 1. Testing scenarios

The testing environment and scenarios are as follows:
- A 3 nodes Vertica database.
- At first, modify a node to type EXECUTE.
- Then verify whether the EXECUTE type node never store data, can query catalog independently, and handle the post part of loading workload from client.
- Finally, verify whether the EXECUTE type node will not involve in other tasks including native load balancing.

The following query results show version and nodes of the test database. 

```bash
VSQL -c "select version()"
               version               
-------------------------------------
 Vertica Analytic Database v10.1.1-0
(1 row)

VSQL -c "select node_name, node_type, node_state from nodes order by 1"
          node_name           | node_type | node_state 
------------------------------+-----------+------------
 v_test_special_node_node0001 | PERMANENT | UP
 v_test_special_node_node0002 | PERMANENT | UP
 v_test_special_node_node0003 | PERMANENT | UP
(3 rows)
```

**Note**, here we will use connection load balancing policies feature which is available since Vertica 9.2.0. So your database version should be not lower than **9.2.0** .

## 2. modify a node to type EXECUTE

 Let's modify node `v_test_special_node_node0003` to type EXECUTE. As we only have 3 nodes here, there will be some warnings about k-safe or high availability. We just ignore them.

```bash
VSQL -c "alter node v_test_special_node_node0003 is EPHEMERAL" 2>/dev/null
ALTER NODE

VSQL -c "select rebalance_cluster()"
 rebalance_cluster 
-------------------
 REBALANCED
(1 row)

VSQL -c "alter node v_test_special_node_node0003 is EXECUTE"
ALTER NODE

VSQL -c "select node_name, node_address, node_type, node_state from nodes order by 1"
          node_name           | node_address | node_type | node_state 
------------------------------+--------------+-----------+------------
 v_test_special_node_node0001 | 172.17.0.11  | PERMANENT | UP
 v_test_special_node_node0002 | 172.17.0.4   | PERMANENT | UP
 v_test_special_node_node0003 | 172.17.0.7   | EXECUTE   | UP
(3 rows)
```


## 3. verify whether the EXECUTE type node never store data, can query catalog independently, and handle the post part of loading workload from client

Let's create a table, load some data, and check how data is stored on nodes. Results show that the EXECUTE type node doesn't store any data.

```BASH
VSQL -c "create table test(id int) segmented by hash(id) all nodes ksafe" 2>/dev/null
CREATE TABLE

seq 1 1000 | VSQL -c "copy test from stdin"
VSQL -c "select count(*) from test"
 count 
-------
  1000
(1 row)

VSQL -c "select node_name, sum(used_bytes) from projection_storage group by 1 order by 1"
          node_name           | sum 
------------------------------+-----
 v_test_special_node_node0001 | 542
 v_test_special_node_node0002 | 560
 v_test_special_node_node0003 |   0
(3 rows)
```

Then repeat 1000 times to query catalog on the EXECUTE node `v_test_special_node_node0003` , check the execution nodes of the query plan. There is only the initiator node, which means it's just the EXECUTE node.

```BASH
for((i=0; i<1000; i++)) ; do \
    VSQL -h 172.17.0.7 -c "explain select * from tables" | grep 'Execute on' ; \
done | sort -u
 |  Execute on: Query Initiator
```

Then repeat 100 times to load data from client on the EXECUTE node `v_test_special_node_node0003` ,  check those operators and execution nodes of the query's profiling info. Results show that the most of works are handled by the EXECUTE node, others just store the final ROS containers.

```BASH
for((i=0; i<100; i++)) ; do \
    VSQL -h 172.17.0.7 -c "explain select * from tables" | grep 'Execute on' ; \
    tid=$(VSQL -h 172.17.0.7 -c "profile copy test from stdin no commit" <<<"$(seq 1001 1100)" 2>&1 | grep '=' | awk -F '=' '{print $2}' | awk '{print $1}')
    VSQL -Aqt -c "select distinct node_name, operator_name from execution_engine_profiles where transaction_id=${tid} and statement_id=1 order by 1, 2"; \
done | sort -u
v_test_special_node_node0001|DataTarget
v_test_special_node_node0001|Recv
v_test_special_node_node0001|Send
v_test_special_node_node0002|DataTarget
v_test_special_node_node0002|Recv
v_test_special_node_node0002|Send
v_test_special_node_node0003|Copy
v_test_special_node_node0003|ExprEval
v_test_special_node_node0003|GroupByNothing
v_test_special_node_node0003|Load
v_test_special_node_node0003|LoadUnion
v_test_special_node_node0003|NewEENode
v_test_special_node_node0003|Recv
v_test_special_node_node0003|Root
v_test_special_node_node0003|Router
v_test_special_node_node0003|Send
v_test_special_node_node0003|Union
v_test_special_node_node0003|ValExpr
```

## 4. verify whether the EXECUTE type node will not involve other tasks including native load balancing

Let's check plan of a business query, results show they only happen on other PERMANENT type nodes, not the EXECUTE type node at all.

```BASH
VSQL -h 172.17.0.11 -c "explain select * from test" \
    | grep 'Execute on:' | awk -F 'Execute on:' '{print $2}' | sort -u
 All Permanent Nodes

VSQL -h 172.17.0.11 -c "explain insert into test select * from test" \
    | grep 'Execute on:' | awk -F 'Execute on:' '{print $2}' | sort -u
 All Permanent Nodes

VSQL -h 172.17.0.7 -c "explain select * from test"  \
    | grep 'Execute on:' | awk -F 'Execute on:' '{print $2}' | sort -u
 All Permanent Nodes

VSQL -h 172.17.0.7 -c "explain insert into test select * from test" \
    | grep 'Execute on:' | awk -F 'Execute on:' '{print $2}' | sort -u
 All Permanent Nodes
```

Then let's set connection load balancing policies excluding the EXECUTE type node,  repeat 1000 times to connect to the database with native load balancing. Result shows that the EXECUTE type node never involve in native load balancing.

```BASH

VSQL -c "select SET_LOAD_BALANCE_POLICY('NONE')"
                         SET_LOAD_BALANCE_POLICY                          
--------------------------------------------------------------------------
 Successfully changed the client initiator load balancing policy to: none
(1 row)
VSQL -c "create network address addr0001 ON v_test_special_node_node0001 WITH '172.17.0.11'"
CREATE NETWORK ADDRESS
VSQL -c "create network address addr0002 ON v_test_special_node_node0002 WITH '172.17.0.4'"
CREATE NETWORK ADDRESS
VSQL -c "create load balance group lbgroup1 with address addr0001, addr0002 policy 'ROUNDROBIN'"
CREATE LOAD BALANCE GROUP
VSQL -c "create routing rule all_client route '0.0.0.0/0' to lbgroup1"
CREATE ROUTING RULE

for((i=0; i<1000; i++)) ; do \
    VSQL -h 172.17.0.11 -B 172.17.0.4 -C -Aqt -c "select node_name from current_session" ; \
done | sort | uniq -c
    500 v_test_special_node_node0001
    500 v_test_special_node_node0002
```
## 5. Conclusion

According result of above experiment, we can confidential say the EXECUTE type node of Vertica can achieve our dedicated node  goal for some special tasks, without impacting others or impacted by others.
