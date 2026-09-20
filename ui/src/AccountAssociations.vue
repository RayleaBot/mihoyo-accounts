<script setup lang="ts">
import{ref}from'vue'
interface Subject{source_protocol:string;source_adapter:string;bot_id:string;actor_id:string}
interface Link{ref:string;primary:Subject;secondary:Subject;created_ms:number}
const props=defineProps<{invoke:<T>(action:string,payload?:Record<string,unknown>)=>Promise<T>;disabled:boolean}>()
const items=ref<Link[]>([]),busy=ref(false),error=ref(''),candidate=ref(''),loaded=ref(false)
function label(s:Subject){return `${s.source_protocol} / ${s.source_adapter} / 机器人${s.bot_id} / 用户${s.actor_id}`}
async function load(){if(busy.value||props.disabled)return;busy.value=true;error.value='';try{items.value=(await props.invoke<{items:Link[]}>('accounts.associations.list')).items;loaded.value=true}catch(e){error.value=e instanceof Error?e.message:'关联列表读取失败。'}finally{busy.value=false}}
async function remove(){busy.value=true;error.value='';try{await props.invoke('accounts.associations.remove',{ref:candidate.value,confirm:true});items.value=items.value.filter(v=>v.ref!==candidate.value);candidate.value=''}catch(e){error.value=e instanceof Error?e.message:'解除关联未完成。'}finally{busy.value=false}}
</script>
<template><section class="account-associations"><div class="section-heading"><h2>本人跨聊天关联</h2><button :disabled="disabled||busy" @click="load">读取关联列表</button></div><p>在已登录的主聊天私聊发送“关联账号”，另一身份发送“申请关联 编号”，最后回到主聊天核对目标并完成确认。关联共用账号记录，CK不复制给其他插件；双方原有账号保留。</p><p>解除关联后，附属身份的后续查询和定时委托停止访问共享账号。恢复账号备份后需重新关联。</p><p v-if="error" role="alert" class="danger-text">{{error}}</p><p v-if="loaded&&!items.length">没有已建立的关联。</p><ul><li v-for="v in items" :key="v.ref"><strong>主身份：{{label(v.primary)}}</strong><p>附属身份：{{label(v.secondary)}}</p><small>{{new Date(v.created_ms).toLocaleString()}} 建立</small><button :disabled="busy||disabled" @click="candidate=v.ref">解除此关联</button><div v-if="candidate===v.ref" class="actions"><span>确认停止上述身份之间的账号共享？</span><button :disabled="busy||disabled" @click="remove">确认解除关联</button><button :disabled="busy" @click="candidate=''">取消</button></div></li></ul></section></template>
<style scoped>.account-associations ul{list-style:none;padding:0;display:grid;gap:16px}.account-associations li{display:grid;gap:8px;border-bottom:1px solid var(--raylea-color-border);padding-block:12px;overflow-wrap:anywhere}</style>
