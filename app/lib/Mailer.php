<?php
use PHPMailer\PHPMailer\PHPMailer;
final class Mailer{
  // Email services used with just an API key (sent over their HTTPS API – no SMTP settings needed)
  // [name, what to paste, where to get it, extra field: ''|secret|domain, regions]
  static function apis(){return [
    'brevo'=>['Brevo (Sendinblue)','API key (starts with xkeysib-)','https://app.brevo.com/settings/keys/api','',[]],
    'sendgrid'=>['SendGrid','API key (starts with SG.)','https://app.sendgrid.com/settings/api_keys','',[]],
    'mailjet'=>['Mailjet','API key','https://app.mailjet.com/account/apikeys','secret',[]],
    'postmark'=>['Postmark','Server API token','https://account.postmarkapp.com/servers','',[]],
    'resend'=>['Resend','API key (starts with re_)','https://resend.com/api-keys','',[]],
    'zeptomail'=>['Zoho ZeptoMail','Send Mail token','https://www.zoho.com/zeptomail/','',[]],
    'mailgun'=>['Mailgun','API key','https://app.mailgun.com/settings/api_security','domain',[]],
    'smtp2go'=>['SMTP2GO','API key (starts with api-)','https://app.smtp2go.com/settings/api_keys/','',[]],
    'elastic'=>['Elastic Email','API key','https://app.elasticemail.com/marketing/settings/new/manage-api','',[]]];}
  // Mailboxes over SMTP: only the email address and its (app) password are asked; [name, host, port, encryption, password hint]
  static function providers(){return [
    'custom'=>['Other / custom SMTP server','','','','Password'],
    'gmail'=>['Gmail / Google Workspace','smtp.gmail.com','587','tls','App password (Google Account → Security → App passwords)'],
    'zoho'=>['Zoho Mail','smtp.zoho.in','465','ssl','Mailbox or app-specific password'],
    'outlook'=>['Microsoft 365 / Outlook','smtp.office365.com','587','tls','Password or app password'],
    'hostinger'=>['Hostinger email','smtp.hostinger.com','465','ssl','Mailbox password'],
    'cpanel'=>['cPanel / my hosting mailbox','','465','ssl','Mailbox password'],
    'ses'=>['Amazon SES (SMTP)','email-smtp.ap-south-1.amazonaws.com','587','tls','SMTP password from SES → SMTP settings']];}
  // Fill server settings for known mailboxes (cPanel: mail.<your domain>)
  static function preset($k){
    $p=self::providers()[$k]??null;if(!$p||$k==='custom')return;
    if(in_array($k,['ses','cpanel'],true)&&setting('smtp_host')!=='')return;
    $host=$k==='cpanel'?'mail.'.substr(strrchr(setting('from_email'),'@')?:'@','1'):$p[1];
    Settings::many(['smtp_host'=>$host,'smtp_port'=>$p[2],'smtp_secure'=>$p[3]]);}
  static function mode(){$m=setting('mail_transport','php');return in_array($m,['php','api','smtp'],true)?$m:'php';}
  static function smtp(){return self::mode()==='smtp';}
  static function label(){$m=self::mode();return $m==='api'?(self::apis()[setting('mail_api')][0]??'API').' API':($m==='smtp'?'SMTP':'PHP mail');}
  // PHP mail() works out of the box on shared hosting; an email service API key or SMTP gives better delivery
  static function ready(){
    $m=self::mode();
    if($m==='api'){$a=self::apis()[setting('mail_api')]??null;return $a&&setting('mail_api_key')!==''&&self::from()!==''&&($a[3]!=='secret'||setting('mail_api_secret')!=='')&&($a[3]!=='domain'||setting('mail_api_domain')!=='');}
    if($m==='smtp')return setting('smtp_host')!==''&&self::user()!=='';
    return function_exists('mail')&&self::from()!=='';}
  static function user(){return setting('smtp_user',setting('from_email'));}
  static function from(){return setting('from_email',self::smtp()?setting('smtp_user'):(self::mode()==='api'?'':setting('company_email')));}
  // $o: msg_id (our Message-ID), headers (In-Reply-To / References for replies in a thread)
  static function send($to,$subject,$html,$text,$files=[],$o=[]){
    if(self::mode()==='api'){
      try{return self::api($to,$subject,$html,$text,$files,(array)($o['headers']??[]));}
      // A service that refuses the threading headers still gets the email out
      catch(Throwable $e){if(empty($o['headers'])||!preg_match('/header|reply|reference/i',$e->getMessage()))throw $e;return self::api($to,$subject,$html,$text,$files,[]);}}
    $m=new PHPMailer(true);
    if(self::smtp()){
      $m->isSMTP();
      $m->Host=setting('smtp_host');$m->Port=(int)setting('smtp_port','465');
      $m->SMTPAuth=true;$m->Username=self::user();$m->Password=setting('smtp_pass');
      $sec=setting('smtp_secure','ssl');$m->SMTPSecure=$sec==='none'?'':$sec;$m->SMTPAutoTLS=$sec!=='none';}
    else{$m->isMail();$m->Sender=self::from();}
    $m->CharSet='UTF-8';$m->Timeout=30;
    $m->setFrom(self::from(),setting('from_name',setting('company_name')));
    if($r=setting('reply_to'))$m->addReplyTo($r);
    if($b=setting('bcc_email'))$m->addBCC($b);
    $m->addAddress($to);
    if(!empty($o['msg_id']))$m->MessageID=$o['msg_id'];
    foreach((array)($o['headers']??[]) as $hk=>$hv)$m->addCustomHeader($hk,$hv);
    $m->Subject=$subject;$m->isHTML(true);$m->Body=$html;$m->AltBody=$text;
    foreach($files as $name=>$path)if(is_file($path))$m->addAttachment($path,$name);
    try{$m->send();}
    catch(Throwable $e){
      if(self::smtp())throw $e;
      // PHP mail() often refuses a "-f" sender the host does not own, or is routed through sendmail – retry both ways
      $ok=false;
      try{$m->Sender='';$m->isMail();$m->send();$ok=true;}catch(Throwable $e2){}
      if(!$ok&&is_executable('/usr/sbin/sendmail')){try{$m->isSendmail();$m->send();$ok=true;}catch(Throwable $e3){}}
      if(!$ok)throw new Exception('Your hosting did not accept the email (PHP mail is disabled or the From email is not allowed). Set the From email to a mailbox on your own domain, or connect an email service in Settings → Messaging → Email. ('.$e->getMessage().')');}
    Usage::log('email',$m->Mailer);
    return $m->getLastMessageID();}
  // Send through the chosen service's HTTPS API with only its API key
  private static function api($to,$subject,$html,$text,$files,$hd=[]){
    $k=setting('mail_api');$key=setting('mail_api_key');$from=self::from();$name=setting('from_name',setting('company_name'));$rt=setting('reply_to');$bcc=setting('bcc_email');
    $att=[];foreach($files as $fn=>$path)if(is_file($path))$att[]=['name'=>$fn,'b64'=>base64_encode(file_get_contents($path)),'type'=>mime_content_type($path)?:'application/octet-stream'];
    $j=fn($u,$body,$h)=>http_json($u,['json'=>$body,'headers'=>$h,'timeout'=>60]);
    switch($k){
      case 'brevo':$r=$j('https://api.brevo.com/v3/smtp/email',['sender'=>['email'=>$from,'name'=>$name],'to'=>[['email'=>$to]],'subject'=>$subject,'htmlContent'=>$html,'textContent'=>$text]+($rt?['replyTo'=>['email'=>$rt]]:[])+($hd?['headers'=>$hd]:[])+($bcc?['bcc'=>[['email'=>$bcc]]]:[])+($att?['attachment'=>array_map(fn($a)=>['name'=>$a['name'],'content'=>$a['b64']],$att)]:[]),['api-key: '.$key,'Accept: application/json']);$id=$r['data']['messageId']??'';break;
      case 'sendgrid':$r=$j('https://api.sendgrid.com/v3/mail/send',['personalizations'=>[['to'=>[['email'=>$to]]]+($bcc?['bcc'=>[['email'=>$bcc]]]:[])],'from'=>['email'=>$from,'name'=>$name],'subject'=>$subject,'content'=>[['type'=>'text/plain','value'=>$text],['type'=>'text/html','value'=>$html]]]+($rt?['reply_to'=>['email'=>$rt]]:[])+($hd?['headers'=>$hd]:[])+($att?['attachments'=>array_map(fn($a)=>['content'=>$a['b64'],'filename'=>$a['name'],'type'=>$a['type']],$att)]:[]),['Authorization: Bearer '.$key]);$id='sg-'.md5($to.microtime());break;
      case 'mailjet':$r=http_json('https://api.mailjet.com/v3.1/send',['json'=>['Messages'=>[['From'=>['Email'=>$from,'Name'=>$name],'To'=>[['Email'=>$to]],'Subject'=>$subject,'TextPart'=>$text,'HTMLPart'=>$html]+($rt?['ReplyTo'=>['Email'=>$rt]]:[])+($hd?['Headers'=>$hd]:[])+($bcc?['Bcc'=>[['Email'=>$bcc]]]:[])+($att?['Attachments'=>array_map(fn($a)=>['ContentType'=>$a['type'],'Filename'=>$a['name'],'Base64Content'=>$a['b64']],$att)]:[])]],'headers'=>['Authorization: Basic '.base64_encode($key.':'.setting('mail_api_secret'))],'timeout'=>60]);$id=$r['data']['Messages'][0]['To'][0]['MessageID']??'';break;
      case 'postmark':$r=$j('https://api.postmarkapp.com/email',['From'=>$name?$name.' <'.$from.'>':$from,'To'=>$to,'Subject'=>$subject,'HtmlBody'=>$html,'TextBody'=>$text,'MessageStream'=>'outbound']+($rt?['ReplyTo'=>$rt]:[])+($hd?['Headers'=>array_map(fn($k,$v)=>['Name'=>$k,'Value'=>$v],array_keys($hd),$hd)]:[])+($bcc?['Bcc'=>$bcc]:[])+($att?['Attachments'=>array_map(fn($a)=>['Name'=>$a['name'],'Content'=>$a['b64'],'ContentType'=>$a['type']],$att)]:[]),['X-Postmark-Server-Token: '.$key,'Accept: application/json']);$id=$r['data']['MessageID']??'';break;
      case 'resend':$r=$j('https://api.resend.com/emails',['from'=>$name?$name.' <'.$from.'>':$from,'to'=>[$to],'subject'=>$subject,'html'=>$html,'text'=>$text]+($rt?['reply_to'=>$rt]:[])+($hd?['headers'=>$hd]:[])+($bcc?['bcc'=>[$bcc]]:[])+($att?['attachments'=>array_map(fn($a)=>['filename'=>$a['name'],'content'=>$a['b64']],$att)]:[]),['Authorization: Bearer '.$key]);$id=$r['data']['id']??'';break;
      case 'zeptomail':$r=$j('https://api.zeptomail.'.(['com'=>'com','eu'=>'eu'][setting('mail_api_region')]??'in').'/v1.1/email',['from'=>['address'=>$from,'name'=>$name],'to'=>[['email_address'=>['address'=>$to]]],'subject'=>$subject,'htmlbody'=>$html,'textbody'=>$text]+($rt?['reply_to'=>[['address'=>$rt]]]:[])+($hd?['mime_headers'=>$hd]:[])+($bcc?['bcc'=>[['email_address'=>['address'=>$bcc]]]]:[])+($att?['attachments'=>array_map(fn($a)=>['content'=>$a['b64'],'mime_type'=>$a['type'],'name'=>$a['name']],$att)]:[]),['Authorization: '.(stripos($key,'zoho-enczapikey')===0?$key:'Zoho-enczapikey '.$key)]);$id=$r['data']['request_id']??'';break;
      case 'mailgun':
        $f=['from'=>$name?$name.' <'.$from.'>':$from,'to'=>$to,'subject'=>$subject,'html'=>$html,'text'=>$text]+($rt?['h:Reply-To'=>$rt]:[])+($bcc?['bcc'=>$bcc]:[]);foreach($hd as $hk=>$hv)$f['h:'.$hk]=$hv;
        $n=0;foreach($files as $fn=>$path)if(is_file($path))$f['attachment'.($n++?'['.$n.']':'')]=new CURLFile($path,mime_content_type($path)?:'application/octet-stream',$fn);
        $r=http_json('https://api.'.(setting('mail_api_region')==='eu'?'eu.':'').'mailgun.net/v3/'.rawurlencode(setting('mail_api_domain')).'/messages',['body'=>$f,'headers'=>['Authorization: Basic '.base64_encode('api:'.$key)],'timeout'=>60]);$id=$r['data']['id']??'';break;
      case 'smtp2go':$r=$j('https://api.smtp2go.com/v3/email/send',['api_key'=>$key,'sender'=>$name?$name.' <'.$from.'>':$from,'to'=>[$to],'subject'=>$subject,'html_body'=>$html,'text_body'=>$text]+($bcc?['bcc'=>[$bcc]]:[])+(($ch=array_merge($rt?[['header'=>'Reply-To','value'=>$rt]]:[],array_map(fn($k,$v)=>['header'=>$k,'value'=>$v],array_keys($hd),$hd)))?['custom_headers'=>$ch]:[])+($att?['attachments'=>array_map(fn($a)=>['filename'=>$a['name'],'fileblob'=>$a['b64'],'mimetype'=>$a['type']],$att)]:[]),[]);$id=$r['data']['data']['email_id']??'';break;
      case 'elastic':$r=$j('https://api.elasticemail.com/v4/emails/transactional',['Recipients'=>['To'=>[$to]]+($bcc?['BCC'=>[$bcc]]:[]),'Content'=>['From'=>$name?$name.' <'.$from.'>':$from,'Subject'=>$subject,'Body'=>[['ContentType'=>'HTML','Content'=>$html],['ContentType'=>'PlainText','Content'=>$text]]]+($rt?['ReplyTo'=>$rt]:[])+($hd?['Headers'=>$hd]:[])+($att?['Attachments'=>array_map(fn($a)=>['BinaryContent'=>$a['b64'],'Name'=>$a['name'],'ContentType'=>$a['type']],$att)]:[])],['X-ElasticEmail-ApiKey: '.$key]);$id=$r['data']['MessageID']??'';break;
      default:throw new Exception('Choose your email service in Settings → Messaging → Email.');}
    $ok=$r['code']>=200&&$r['code']<300&&!($k==='smtp2go'&&!empty($r['data']['data']['error']));
    Usage::log('email','api_'.$k,1,0,0,null,!$ok);
    if(!$ok){$d=(array)($r['data']??[]);$msg='';foreach([['error','details',0,'message'],['ErrorMessage'],['Messages',0,'Errors',0,'ErrorMessage'],['errors',0,'message'],['data','error'],['message'],['Message'],['Error'],['error']] as $pth){$v=$d;foreach($pth as $x)$v=is_array($v)?($v[$x]??null):null;if(is_string($v)&&$v!==''){$msg=$v;break;}}throw new Exception(self::apis()[$k][0].' refused the email (HTTP '.$r['code'].'): '.($msg!==''?$msg:($r['error']?:mb_substr($r['body'],0,200))).($r['code']===401||$r['code']===403?' – check the API key and that the From email / domain is verified in your '.self::apis()[$k][0].' account.':''));}
    return $id?:'api-'.token(6);}
  static function html($text,$pixel=''){
    $acc=setting('brand_accent','#B8860B');
    $t=nl2br(e($text));
    $t=preg_replace('~(https?://[^\s<]+)~','<a href="$1" style="color:'.$acc.'">$1</a>',$t);
    [$lf,$chip]=Report::logoPick('#FFFFFF');$logo=$lf?base_url($lf):'';
    return '<div style="font-family:Arial,Helvetica,sans-serif;font-size:14px;line-height:1.6;color:#1a1a1a;max-width:600px">'.$t.'<br><br><table cellpadding="0" cellspacing="0" style="border-top:2px solid '.$acc.';padding-top:10px"><tr>'.($logo?'<td style="'.($chip?'background:'.$chip.';padding:8px;border-radius:6px':'padding:0 4px 0 0').'"><img src="'.e($logo).'" height="48" alt=""></td>':'').'<td style="padding-left:12px;font-size:12px;color:#555"><b style="color:#0b0b0b">'.e(setting('company_name')).'</b><br>'.e(setting('company_phone')).' · '.e(setting('company_website')).'</td></tr></table>'.($pixel?'<img src="'.e($pixel).'" width="1" height="1" alt="" style="display:block">':'').'</div>';}
  // ---- Inbox (IMAP, no php-imap extension needed) ----
  static function imapOn(){return setting('imap_on',setting('imap_host')!==''?'1':'0')==='1'&&setting('imap_host')!=='';}
  private static function imap(){
    $host=setting('imap_host');$port=(int)setting('imap_port','993');
    $fp=@stream_socket_client(($port===993?'ssl://':'tcp://').$host.':'.$port,$en,$es,20);
    if(!$fp)throw new Exception('Could not connect to the inbox ('.$host.':'.$port.'): '.$es);
    stream_set_timeout($fp,40);$n=0;
    $cmd=function($c)use($fp,&$n){$tag='A'.(++$n);fwrite($fp,$tag.' '.$c."\r\n");$out='';while(($l=fgets($fp))!==false){$out.=$l;if(strpos($l,$tag.' ')===0)break;}return $out;};
    fgets($fp);
    $q=fn($s)=>'"'.addcslashes($s,'"\\').'"';
    $r=$cmd('LOGIN '.$q(setting('imap_user',setting('smtp_user',setting('from_email')))).' '.$q(setting('imap_pass',setting('smtp_pass'))));
    if(!preg_match('/^A\d+ OK/m',$r)){fclose($fp);throw new Exception('Inbox login failed – check the IMAP login and password.');}
    return [$fp,$cmd];}
  static function testInbox(){[$fp,$cmd]=self::imap();$r=$cmd('SELECT INBOX');$cmd('LOGOUT');fclose($fp);return preg_match('/\* (\d+) EXISTS/',$r,$m)?(int)$m[1]:0;}
  // New messages since the last check (parsed). Remembers the last UID it saw.
  static function fetchNew($max=40){
    [$fp,$cmd]=self::imap();
    $sel=$cmd('SELECT INBOX');$uv=preg_match('/UIDVALIDITY (\d+)/',$sel,$m)?$m[1]:'';
    $last=(int)setting('imap_uid','0');if(setting('imap_uidv')!==$uv)$last=0;
    $r=$last?$cmd('UID SEARCH UID '.($last+1).':*'):$cmd('UID SEARCH SINCE '.date('d-M-Y',strtotime('-14 days')));
    $uids=preg_match('/\* SEARCH ([\d ]+)/',$r,$m)?array_map('intval',explode(' ',trim($m[1]))):[];
    $uids=array_values(array_filter($uids,fn($u)=>$u>$last));sort($uids);$uids=array_slice($uids,0,$max);
    $out=[];
    foreach($uids as $uid){
      $raw=$cmd('UID FETCH '.$uid.' (BODY.PEEK[]<0.400000>)');
      if(preg_match('/\{(\d+)\}\r\n/',$raw,$mm,PREG_OFFSET_CAPTURE)){$msg=substr($raw,$mm[0][1]+strlen($mm[0][0]),(int)$mm[1][0]);$p=self::parse($msg);$p['uid']=$uid;$out[]=$p;}
      $last=max($last,$uid);}
    if($uids||setting('imap_uidv')!==$uv){Settings::set('imap_uid',(string)$last);Settings::set('imap_uidv',$uv);}
    $cmd('LOGOUT');fclose($fp);
    return $out;}
  // Small MIME parser: headers, best text body (plain, else HTML → text), without quoted earlier messages
  static function parse($raw){
    [$h,$b]=array_pad(preg_split("/\r?\n\r?\n/",$raw,2),2,'');
    $H=self::headers($h);$body=self::body($H,$b);
    $from=$H['from']??'';preg_match('/<([^>]+)>/',$from,$fm);$addr=strtolower(trim($fm[1]??$from,' "<>'));
    $name=trim(preg_replace('/<[^>]*>/','',$from),' "');
    return ['from'=>$addr,'from_name'=>$name,'to'=>$H['to']??'','subject'=>$H['subject']??'(no subject)','date'=>($t=strtotime(preg_replace('/^[A-Za-z]{3},\s*/','',$H['date']??'')))?date('Y-m-d H:i:s',$t):date('Y-m-d H:i:s'),'msg_id'=>trim($H['message-id']??''),'in_reply_to'=>trim($H['in-reply-to']??''),'references'=>trim($H['references']??''),'auto'=>isset($H['auto-submitted'])&&stripos($H['auto-submitted'],'no')!==0||stripos($H['precedence']??'','bulk')!==false,'text'=>self::strip($body)];}
  private static function headers($h){
    $o=[];foreach(preg_split("/\r?\n(?![ \t])/",$h) as $l){if(!strpos($l,':'))continue;[$k,$v]=explode(':',$l,2);$k=strtolower(trim($k));$v=trim(preg_replace('/\r?\n[ \t]+/',' ',$v));$o[$k]=in_array($k,['subject','from','to'],true)?self::dec($v):$v;}
    return $o;}
  private static function dec($v){$d=@iconv_mime_decode($v,ICONV_MIME_DECODE_CONTINUE_ON_ERROR,'UTF-8');return $d!==false?$d:mb_decode_mimeheader($v);}
  private static function body($H,$b){
    $ct=$H['content-type']??'text/plain';
    if(preg_match('~multipart/~i',$ct)&&preg_match('/boundary="?([^";]+)"?/i',$ct,$m)){
      $parts=explode('--'.$m[1],$b);$plain='';$html='';
      foreach($parts as $pt){$pt=ltrim($pt,"\r\n");if($pt===''||strpos($pt,'--')===0)continue;[$ph,$pb]=array_pad(preg_split("/\r?\n\r?\n/",$pt,2),2,'');$PH=self::headers($ph);
        if(stripos($PH['content-disposition']??'','attachment')!==false)continue;
        $pct=$PH['content-type']??'text/plain';
        if(preg_match('~multipart/~i',$pct)){$t=self::body($PH,$pb);if($t!==''&&$plain==='')$plain=$t;continue;}
        if(stripos($pct,'text/plain')!==false&&$plain==='')$plain=self::decode($PH,$pb);
        elseif(stripos($pct,'text/html')!==false&&$html==='')$html=self::decode($PH,$pb);}
      return $plain!==''?$plain:self::html2text($html);}
    $t=self::decode($H,$b);return stripos($ct,'text/html')!==false?self::html2text($t):$t;}
  private static function decode($H,$b){
    $enc=strtolower($H['content-transfer-encoding']??'');
    if($enc==='base64')$b=base64_decode(preg_replace('/\s+/','',$b));elseif($enc==='quoted-printable')$b=quoted_printable_decode($b);
    $cs=preg_match('/charset="?([^";\s]+)/i',$H['content-type']??'',$m)?strtoupper($m[1]):'UTF-8';
    if($cs!=='UTF-8'&&$cs!=='US-ASCII'){$c=@mb_convert_encoding($b,'UTF-8',$cs);if($c!==false)$b=$c;}
    return $b;}
  private static function html2text($h){$h=preg_replace('~<(script|style|head)[^>]*>.*?</\1>~is','',$h);$h=preg_replace('~<blockquote.*$~is','',$h);$h=preg_replace('~<div class="?gmail_quote.*$~is','',$h);$h=preg_replace('~<(br|/p|/div|/li|/tr|/h\d)[^>]*>~i',"\n",$h);return trim(html_entity_decode(strip_tags($h),ENT_QUOTES|ENT_HTML5,'UTF-8'));}
  // Cut the quoted earlier conversation ("On … wrote:", "> …", "-----Original Message-----")
  static function strip($t){
    $t=str_replace("\r\n","\n",(string)$t);
    foreach(['/^On .{4,200}wrote:\s*$/m','/^-{2,}\s*Original Message\s*-{2,}/mi','/^_{10,}\s*$/m','/^From: .+\nSent: /m','/^\s*>/m'] as $re)if(preg_match($re,$t,$m,PREG_OFFSET_CAPTURE)&&$m[0][1]>0)$t=substr($t,0,$m[0][1]);
    return trim(preg_replace("/\n{3,}/","\n\n",$t));}}
