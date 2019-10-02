# 居然给客户推荐在大数据分析数据库中用触发器？What's your problem?!

                          刘定强 2019.07.03

最近又碰到某 **G**姓(请勿对号入座) 分析数据库给客户吹牛：“我们有触发器，xxx没有”！

有段时间没碰到这么莫名自豪的了。

了解关系数据库的人都知道，触发器主要用在插入、修改和删除数据操作过程中自动触发一定的业务规则，多用于维护数据一致性等。但它会给系统带来不必要复杂性(比如多个表上的触发器被级联触发)，维护和问题诊断非常麻烦，更严重的是会给繁忙系统带来致命的的性能问题。有经验的架构师和数据库管理员都会禁止使用触发器，哪怕是在只处理单条数据的OLTP(在线事务处理系统，如订单、账务、计费系统等)系统中也会极其慎重; 在OLAP(在线分析系统，如数据库仓库、商务智能和报表系统等)和大数据分析系统里更是它的禁区。

可是 **G** 家的官方文档真的会推荐在大数据分析系统中用触发器吗？不用想就知道肯定不会。怎么能因为自己拿开源MySQL/PostgreSQL简单攒了个并行数据库来做数据分析，又舍不得下功夫把屁股擦干净，就拿这种垃圾当特性来忽悠别人呢？正经人干不出这种低级的事。

不尊重常识、不按最佳实践胡扯坑人就是在耍流氓！

您再听到这样的人忽悠，还需要理他们吗？
