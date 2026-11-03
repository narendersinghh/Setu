<?php
// Emails: every email to and from leads in one place – conversations per lead, replies pulled from the inbox, answer from here
$title='Emails';$subtitle='Every email with your leads – read replies and answer without leaving Setu';
$vis=(can('prospecting')&&mod('prospecting')?'':' AND l.is_lead=1').scope();
$lid=(int)($_GET['lead']??$_POST['lead']??0);
$lead=$lid?DB::row("SELECT l.* FROM leads l WHERE l.id=?$vis",[$lid]):null;
if(is_post()){
  $a=$_POST['a']??'';
  try{
    if($a==='sync'){$n=Outreach::syncInbox();flash($n?$n.' new email(s) received.':'No new emails.');}
    if($a==='send'){
      if(!$lead)throw new Exception('Choose a lead.');
      $to=trim($_POST['to']??'')?:$lead['email'];
      if(!filter_var($to,FILTER_VALIDATE_EMAIL))throw new Exception('This lead has no valid email address.');
      if(!Mailer::ready())throw new Exception('Email is not set up yet – Settings → Messaging → Email.');
      $vars=lead_vars($lead);$sub=trim(tpl($_POST['subject']??'',$vars));$text=trim(tpl($_POST['body']??'',$vars));
      if($sub===''||$text==='')throw new Exception('Write a subject and a message.');
      $att=[];if(!empty($_POST['attach'])&&DB::val('SELECT id FROM reports WHERE lead_id=?',[$lead['id']]))foreach(Outreach::files($lead) as $f)$att[$f['name']]=$f['path'];
      $rep=(int)($_POST['reply']??0)?DB::row("SELECT * FROM messages WHERE id=? AND lead_id=? AND channel='email'",[(int)$_POST['reply'],$lead['id']]):null;
      Outreach::email($lead['id'],$to,$sub,$text,$att,$rep);
      if(!in_array($lead['email_status'],['sent','opened'],true))DB::update('leads',['email_status'=>'sent'],'id=?',[$lead['id']]);
      event($lead['id'],'sent','Email sent by '.user()['name'].': '.$sub,'email');
      flash('Email sent to '.e($to).'.');}
  }catch(Throwable $e){flash(e($e->getMessage()),'err');}
  redirect(url('emails',array_filter(['lead'=>$lead['id']??0,'f'=>$_GET['f']??''])));}
$f=$_GET['f']??'';$q=trim($_GET['q']??'');
$w="m.channel='email'".$vis;$args=[];
if($q!==''){$w.=' AND (l.name LIKE ? OR l.contact_name LIKE ? OR l.email LIKE ? OR m.subject LIKE ?)';array_push($args,...array_fill(0,4,'%'.$q.'%'));}
$hav=['unread'=>'HAVING un>0','replied'=>'HAVING ins>0','waiting'=>'HAVING ins=0'][$f]??'';
$conv=DB::all("SELECT l.id,l.name,l.contact_name,l.email,MAX(m.id) mid,MAX(m.created_at) at,SUM(m.direction='in' AND m.is_read=0) un,SUM(m.direction='in') ins,COUNT(*) n FROM messages m JOIN leads l ON l.id=m.lead_id WHERE $w GROUP BY l.id $hav ORDER BY at DESC LIMIT 150",$args);
$last=[];if($conv){[$in,$v]=DB::in(array_column($conv,'mid'));foreach(DB::all("SELECT id,direction,subject,body FROM messages WHERE id IN ($in)",$v) as $r)$last[$r['id']]=$r;}
$cnt=DB::row("SELECT COUNT(DISTINCT m.lead_id) t,COUNT(DISTINCT CASE WHEN m.direction='in' AND m.is_read=0 THEN m.lead_id END) un FROM messages m JOIN leads l ON l.id=m.lead_id WHERE m.channel='email'".$vis);
$thread=[];$rep=null;$hasRep=false;
if($lead){
  $thread=DB::all("SELECT * FROM messages WHERE lead_id=? AND channel='email' ORDER BY created_at,id",[$lead['id']]);
  DB::q("UPDATE messages SET is_read=1 WHERE lead_id=? AND channel='email' AND direction='in' AND is_read=0",[$lead['id']]);
  foreach($thread as $m)if($m['direction']==='in'||!$rep)$rep=$m;
  $hasRep=report_mode()!=='link'&&DB::val('SELECT id FROM reports WHERE lead_id=?',[$lead['id']]);}
$sub=$rep?'Re: '.preg_replace('/^((re|fwd?):\s*)+/i','',$rep['subject']):'';
$ready=Mailer::ready();$imap=Mailer::imapOn();$canSet=can('settings');
$pick=DB::all("SELECT l.id,l.name,l.contact_name,l.email FROM leads l WHERE l.email<>''$vis ORDER BY COALESCE(l.last_activity,l.created_at) DESC LIMIT 400");
$ago=fn($t)=>date('Y-m-d',strtotime($t))===date('Y-m-d')?date('h:i A',strtotime($t)):date('d M',strtotime($t));
$tabs=[''=>['All',(int)$cnt['t']],'unread'=>['Unread',(int)$cnt['un']],'replied'=>['Replied',null],'waiting'=>['Awaiting reply',null]];
?>
<?php if(!$ready):?><div class="al al-warn"><?=icon('alert')?><span><b>Email is not set up yet.</b> <?=$canSet?'<a class="lnk" href="'.url('settings',['tab'=>'email']).'">Connect your email service or mailbox</a> to send from here.':'Ask your admin to connect email in Settings.'?></span></div><?php elseif(!$imap):?><div class="al al-info"><?=icon('mail')?><span><b>Replies are not coming in yet.</b> Turn on <i>Detect replies by reading your inbox (IMAP)</i> <?=$canSet?'in <a class="lnk" href="'.url('settings',['tab'=>'email']).'">Settings → Email</a>':'in Settings → Email'?> with the mailbox your leads reply to (your From or Reply-to address). Setu then brings every reply here automatically.</span></div><?php endif?>
<div class="ib-w<?=$lead?' has-t':''?>">
<div class="card p0 ib-l"><div class="ib-lh"><form class="ib-s" method="get"><input type="hidden" name="r" value="emails"><input type="hidden" name="f" value="<?=e($f)?>"><input name="q" value="<?=e($q)?>" placeholder="Search name, email, subject"></form><button type="button" class="btn btn-p btn-sm" onclick="A.modal('m-new')" title="New email"><?=icon('plus')?>New</button></div>
<div class="ib-f"><?php foreach($tabs as $k=>[$lb,$n]):?><a href="<?=url('emails',array_filter(['f'=>$k,'q'=>$q]))?>" class="<?=$f===$k?'on':''?>"><?=e($lb)?><?=$n?'<em>'.$n.'</em>':''?></a><?php endforeach?></div>
<div class="ib-c"><?php if(!$conv):?><div class="empty sm"><?=icon('mail')?><p><?=$q!==''||$f?'No conversations match.':'No emails yet. Emails sent to leads – reports, automations and your own – appear here.'?></p></div><?php endif?>
<?php foreach($conv as $c):$lm=$last[$c['mid']]??null;?><a class="ib-i<?=$lead&&(int)$lead['id']===(int)$c['id']?' on':''?><?=$c['un']?' un':''?>" href="<?=url('emails',array_filter(['lead'=>$c['id'],'f'=>$f,'q'=>$q]))?>"><span class="avc c-<?=['indigo','violet','blue','teal','green','amber','orange','pink'][$c['id']%8]?>"><?=e(mb_strtoupper(mb_substr($c['contact_name']?:$c['name'],0,1)))?></span><span class="grow"><span class="ib-n"><b><?=e($c['contact_name']?:$c['name'])?></b><small><?=$ago($c['at'])?></small></span><span class="ib-sub"><?=$lm&&$lm['direction']==='out'?'You: ':''?><?=e(mb_strimwidth($lm['subject']??'',0,60,'…'))?></span><span class="ib-p"><?=e(mb_strimwidth(preg_replace('/\s+/',' ',(string)($lm['body']??'')),0,80,'…'))?></span></span><?php if($c['un']):?><span class="ib-b"><?=(int)$c['un']?></span><?php endif?></a><?php endforeach?></div></div>
<div class="card p0 ib-t"><?php if(!$lead):?><div class="empty"><?=icon('mail','eic')?><h4>Select a conversation</h4><p>Pick a lead on the left, or start a <a class="lnk" href="#" onclick="A.modal('m-new');return false">new email</a>.</p><?php if($imap):?><form method="post" class="mt"><?=csrf_field()?><input type="hidden" name="a" value="sync"><button class="btn btn-sm"><?=icon('refresh')?>Check for new emails</button></form><?php endif?></div><?php else:?>
<div class="ib-th"><a class="ib-bk" href="<?=url('emails',array_filter(['f'=>$f,'q'=>$q]))?>" title="Back">‹</a><div class="grow"><b><?=e($lead['contact_name']?:$lead['name'])?></b><small><?=e($lead['email'])?><?=$lead['contact_name']?' · '.e($lead['name']):''?></small></div><?php if($imap):?><form method="post"><?=csrf_field()?><input type="hidden" name="a" value="sync"><input type="hidden" name="lead" value="<?=$lead['id']?>"><button class="ib bd" title="Check for new emails"><?=icon('refresh')?></button></form><?php endif?><a class="btn btn-sm" href="<?=url('lead',['id'=>$lead['id']])?>">Open lead</a></div>
<div class="ib-ms" id="ib-ms"><?php if(!$thread):?><div class="empty sm"><p>No emails with this lead yet – write the first one below.</p></div><?php endif?><?php foreach($thread as $m):$in=$m['direction']==='in';?><div class="em <?=$in?'em-in':'em-out'?>"><div class="em-h"><b><?=$in?e($lead['contact_name']?:$lead['name']):'You'?></b><span class="muted"><?=date('d M Y, h:i A',strtotime($m['created_at']))?></span><?php if(!$in):?><?=chbadge($m['status'])?><?php endif?></div><div class="em-s"><?=e($m['subject'])?></div><div class="em-b"><?=nl2br(e(trim((string)$m['body'])))?></div></div><?php endforeach?></div>
<form method="post" class="ib-r"><?=csrf_field()?><input type="hidden" name="a" value="send"><input type="hidden" name="lead" value="<?=$lead['id']?>"><input type="hidden" name="reply" value="<?=(int)($rep['id']??0)?>"><input name="subject" value="<?=e($sub)?>" placeholder="Subject" required><textarea name="body" rows="4" placeholder="Write your reply… ({{first_name}}, {{booking_link}}, {{report_link}} work here)" required></textarea><div class="ib-ra"><?php if($hasRep):?><label class="chk sm"><input type="checkbox" name="attach" value="1"> Attach the audit report PDF</label><?php endif?><button class="btn btn-p" <?=$ready&&$lead['email']?'':'disabled'?>><?=icon('send')?>Send</button></div></form>
<?php endif?></div></div>
<div class="md" id="m-new"><form method="post" class="md-b"><?=csrf_field()?><input type="hidden" name="a" value="send"><div class="md-h"><h3><span class="hi c-blue"><?=icon('mail')?></span>New email</h3><button type="button" class="ib" onclick="A.close()"><?=icon('x')?></button></div><label>To (lead)<select name="lead" required><option value="">Choose a lead…</option><?php foreach($pick as $x):?><option value="<?=$x['id']?>"><?=e(($x['contact_name']?:$x['name']).' – '.$x['email'])?></option><?php endforeach?></select></label><label>Subject<input name="subject" required></label><label>Message<textarea name="body" rows="7" required placeholder="Hi {{first_name}}, …"></textarea><small>Placeholders: {{first_name}} {{business_name}} {{booking_link}} {{report_link}} {{sender_name}}</small></label><label class="chk sm"><input type="checkbox" name="attach" value="1"> Attach the audit report PDF (when the lead has one)</label><div class="card-f"><button class="btn btn-p" <?=$ready?'':'disabled'?>><?=icon('send')?>Send</button></div></form></div>
<script>(function(){var m=document.getElementById('ib-ms');if(m)m.scrollTop=m.scrollHeight;<?php if($imap):?>var last=<?=(int)setting('imap_last','0')?>,now=<?=time()?>;function sync(){A.post('index.php?r=api&do=mail_sync',{}).then(function(r){if(r&&r.n>0&&!document.querySelector('.md.on')&&!(document.activeElement&&/INPUT|TEXTAREA/.test(document.activeElement.tagName)&&document.activeElement.value))location.reload();setTimeout(sync,60000)}).catch(function(){setTimeout(sync,120000)})}setTimeout(sync,now-last>60?500:60000);<?php endif?>})()</script>
