## 一个为方便测试同学而写的发布小程序服务

### 功能
- 支持小程序预览和发布
- 支持发布成功企业微信通知相关人
- 支持发送小程序二维码到企业微信群

### 配置
- 配置git相关地址、用户名、密码
- 配置企业微信webhook
- 配置需要在群里@的人
- 修改index.html里的项目为真实的git项目名称(select option里的value值)

### 操作
1. 打开微信小程序IDE服务端口
2. 把小程序项目二维码放置到assets，命名与git项目名称相同
3. 点击登录IDE
4. 扫码登录
5. 选择项目，输入分支名称，点击预览或发布
