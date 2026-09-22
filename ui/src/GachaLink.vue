<script setup lang="ts">
import{computed,ref}from'vue'
import type{Account}from'./model'
import{sealCredentialInput}from'./credential-input'
const props=defineProps<{account:Account;busy:boolean;invoke:<T>(action:string,payload?:Record<string,unknown>)=>Promise<T>}>()
const emit=defineEmits<{changed:[];working:[boolean]}>()
const roles=computed(()=>props.account.roles.filter(r=>r.game==='starrail'))
const roleRef=ref(''),value=ref(''),working=ref(false),error=ref(''),notice=ref('')
async function save(){const role=roleRef.value||roles.value[0]?.ref;if(!role||!value.value.trim()||working.value||props.busy)return;working.value=true;emit('working',true);error.value='';notice.value='';try{const {public_key}=await props.invoke<{public_key:JsonWebKey}>('vault.public_key');const box=await sealCredentialInput(value.value.trim(),public_key,'account.gacha.link');value.value='';const out=await props.invoke<{uid:string;expires_ms:number}>('accounts.gacha_link.save',{account_ref:props.account.ref,role_ref:role,revision:props.account.revision,box});notice.value=`UID ${out.uid} 的抽卡链接已加密保存，${new Date(out.expires_ms).toLocaleString()} 前可同步跃迁记录。`;emit('changed')}catch(e){error.value=e instanceof Error?e.message:'抽卡链接未保存。'}finally{value.value='';working.value=false;emit('working',false)}}
</script>
<template><details v-if="account.realm!=='os'&&roles.length" class="gacha-link"><summary>星铁抽卡链接</summary><form @submit.prevent="save"><p>国服星铁无法用登录凭据换取跃迁记录授权。在游戏内打开跃迁记录后复制链接，粘贴到这里加密保存，一天内可用“更新抽卡记录”同步。链接不会进入游戏插件或聊天。</p><label v-if="roles.length>1">角色<select v-model="roleRef"><option v-for="r in roles" :key="r.ref" :value="r.ref">{{r.nickname}} · {{r.uid}}</option></select></label><label>抽卡链接<input v-model="value" type="password" autocomplete="off" maxlength="16384" :disabled="busy||working"></label><button :disabled="busy||working||!value.trim()">加密保存链接</button><p v-if="error" class="danger-text" role="alert">{{error}}</p><p v-if="notice" role="status">{{notice}}</p></form></details></template>
<style scoped>.gacha-link{border-top:1px solid var(--raylea-color-border);padding-top:16px}.gacha-link summary{cursor:pointer;font-weight:600}.gacha-link form{display:grid;gap:14px;margin-top:14px}.gacha-link label{display:grid;gap:8px}</style>
