package api

import "bytes"

// managementProxyPanelScript adds the reusable proxy selector to the generated
// management control panel. Keeping this small integration outside the generated
// asset means a future panel update does not remove proxy account management.
const managementProxyPanelScript = `(function(){
  var panel=document.createElement("section");
  panel.id="cpa-proxy-panel";
  panel.style.cssText="position:fixed;right:18px;bottom:18px;z-index:9999;width:320px;padding:14px;border:1px solid #d1d5db;border-radius:12px;background:var(--bg-primary,#fff);box-shadow:0 8px 30px #0002;color:var(--text-primary,#111827);font:13px system-ui,sans-serif";
  panel.innerHTML="<h3 style='margin:0 0 10px;font-size:15px'>代理账号管理</h3><label>认证账号</label><select id='cpa-proxy-auth' style='box-sizing:border-box;width:100%;padding:6px 8px;border:1px solid #d1d5db;border-radius:6px'><option value=''>请选择认证账号</option></select><label style='display:block;margin:8px 0 4px'>代理账号</label><select id='cpa-proxy-select' style='box-sizing:border-box;width:100%;padding:6px 8px;border:1px solid #d1d5db;border-radius:6px'><option value=''>直连（不使用代理）</option></select><div style='display:flex;gap:6px;margin-top:9px'><button id='cpa-proxy-apply' type='button'>绑定到账号</button><button id='cpa-proxy-refresh' type='button'>刷新</button></div><details style='margin-top:10px;border-top:1px solid #e5e7eb;padding-top:8px'><summary>新增代理账号</summary><label>名称</label><input id='cpa-proxy-name' placeholder='例如：香港出口 01' style='width:100%;box-sizing:border-box'><label>协议 / 主机 / 端口</label><div style='display:flex;gap:4px'><select id='cpa-proxy-protocol'><option>http</option><option>https</option><option>socks5</option><option>socks5h</option></select><input id='cpa-proxy-host' placeholder='127.0.0.1' style='flex:1'><input id='cpa-proxy-port' type='number' min='1' max='65535' placeholder='8080' style='width:65px'></div><label>用户名 / 密码（可选）</label><div style='display:flex;gap:4px'><input id='cpa-proxy-user' style='flex:1'><input id='cpa-proxy-password' type='password' style='flex:1'></div><button id='cpa-proxy-create' type='button' style='margin-top:8px'>创建代理账号</button></details><div id='cpa-proxy-status' style='min-height:18px;margin-top:8px'></div>";
  document.body.appendChild(panel);
  var authSelect=panel.querySelector("#cpa-proxy-auth"),proxySelect=panel.querySelector("#cpa-proxy-select"),status=panel.querySelector("#cpa-proxy-status"),state={token:"",proxies:[],files:[]};
  function message(text,error){status.textContent=text||"";status.style.color=error?"#b91c1c":""}
  function capture(headers){if(!headers)return;var value=headers instanceof Headers?headers.get("Authorization"):headers.Authorization||headers.authorization;if(value&&/^Bearer\\s+/i.test(value))state.token=value.replace(/^Bearer\\s+/i,"")}
  var nativeFetch=window.fetch.bind(window);window.fetch=function(input,init){capture(init&&init.headers);return nativeFetch(input,init)};
  var nativeOpen=XMLHttpRequest.prototype.open,nativeHeader=XMLHttpRequest.prototype.setRequestHeader;XMLHttpRequest.prototype.open=function(method,url){this.__cpaUrl=url;return nativeOpen.apply(this,arguments)};XMLHttpRequest.prototype.setRequestHeader=function(name,value){if(String(name).toLowerCase()==="authorization")capture({Authorization:value});return nativeHeader.apply(this,arguments)};
  function api(path,options){options=options||{};options.headers=Object.assign({"Content-Type":"application/json"},options.headers||{});if(state.token)options.headers.Authorization="Bearer "+state.token;return nativeFetch(path,options).then(function(response){return response.json().catch(function(){return null}).then(function(body){if(!response.ok)throw new Error(body&&body.error||"请求失败（"+response.status+"）");return body})})}
  function render(){proxySelect.innerHTML="<option value=''>直连（不使用代理）</option>";state.proxies.forEach(function(proxy){var option=document.createElement("option");option.value=proxy.id;option.textContent=(proxy.name||proxy.id)+" · "+proxy.protocol+"://"+proxy.host+":"+proxy.port;proxySelect.appendChild(option)});var current=authSelect.value;authSelect.innerHTML="<option value=''>请选择认证账号</option>";state.files.filter(function(file){return file&&file.name}).forEach(function(file){var option=document.createElement("option");option.value=file.name;option.textContent=(file.email||file.label||file.name)+(file.proxy_name?" · "+file.proxy_name:"");option.dataset.proxyId=file.proxy_id||"";authSelect.appendChild(option)});if(current)authSelect.value=current}
  function load(){if(!state.token)return;return Promise.all([api("/v0/management/proxy-accounts"),api("/v0/management/auth-files")]).then(function(result){state.proxies=result[0].proxies||result[0].proxy_accounts||[];state.files=result[1].files||[];render();panel.hidden=false;message("")}).catch(function(error){message(error.message,true)})}
  authSelect.addEventListener("change",function(){var option=authSelect.selectedOptions[0];proxySelect.value=option&&option.dataset.proxyId||""});panel.querySelector("#cpa-proxy-refresh").addEventListener("click",load);
  panel.querySelector("#cpa-proxy-apply").addEventListener("click",function(){if(!authSelect.value){message("请先选择认证账号",true);return}api("/v0/management/auth-files/fields",{method:"PATCH",body:JSON.stringify({name:authSelect.value,proxy_id:proxySelect.value})}).then(function(){message("代理绑定成功");return load()}).catch(function(error){message(error.message,true)})});
  panel.querySelector("#cpa-proxy-create").addEventListener("click",function(){var account={name:panel.querySelector("#cpa-proxy-name").value.trim(),protocol:panel.querySelector("#cpa-proxy-protocol").value,host:panel.querySelector("#cpa-proxy-host").value.trim(),port:Number(panel.querySelector("#cpa-proxy-port").value),username:panel.querySelector("#cpa-proxy-user").value.trim(),password:panel.querySelector("#cpa-proxy-password").value};api("/v0/management/proxy-accounts",{method:"POST",body:JSON.stringify(account)}).then(function(){message("代理账号创建成功");return load()}).catch(function(error){message(error.message,true)})});
  setInterval(load,5000);setTimeout(load,250);
})();`

func injectManagementProxyPanel(data []byte) []byte {
	marker := []byte("</body>")
	idx := bytes.LastIndex(bytes.ToLower(data), marker)
	if idx < 0 {
		return data
	}
	script := append([]byte("<script>"), managementProxyPanelScript...)
	script = append(script, []byte("</script>")...)
	out := make([]byte, 0, len(data)+len(script))
	out = append(out, data[:idx]...)
	out = append(out, script...)
	out = append(out, data[idx:]...)
	return out
}
