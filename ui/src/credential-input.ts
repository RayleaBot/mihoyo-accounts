export interface InputBox {key:string;nonce:string;payload:string}
function base64(value:ArrayBuffer|Uint8Array){return btoa(String.fromCharCode(...new Uint8Array(value instanceof Uint8Array?value.buffer:value)))}
export async function sealCredentialInput(value:string,publicKey:JsonWebKey,purpose:string):Promise<InputBox>{
 if(!globalThis.crypto?.subtle)throw new Error('当前连接无法加密凭据，请通过 HTTPS 或本机地址打开管理页。')
 const plain=new TextEncoder().encode(value),label=new TextEncoder().encode('raylea/credential-input/v1/'+purpose)
 if(plain.length>8192)throw new Error('输入内容过长。')
 let rawKey:Uint8Array<ArrayBuffer>|undefined
 try{
  const key=await crypto.subtle.generateKey({name:'AES-GCM',length:256},true,['encrypt'])
  rawKey=new Uint8Array(await crypto.subtle.exportKey('raw',key))
  const rsa=await crypto.subtle.importKey('jwk',publicKey,{name:'RSA-OAEP',hash:'SHA-256'},false,['encrypt']),nonce=crypto.getRandomValues(new Uint8Array(12))
  const wrapped=await crypto.subtle.encrypt({name:'RSA-OAEP',label},rsa,rawKey),encrypted=await crypto.subtle.encrypt({name:'AES-GCM',iv:nonce,additionalData:label},key,plain)
  return {key:base64(wrapped),nonce:base64(nonce),payload:base64(encrypted)}
 }finally{plain.fill(0);rawKey?.fill(0)}
}
