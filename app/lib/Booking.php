<?php
// Appointment slots, public booking page and reminders
final class Booking{
  static function link($lead=null){return !empty($lead['token'])?base_url('book.php?t='.$lead['token']):base_url('book.php?g='.setting('booking_key'));}
  static function slots($days=14){
    $wd=array_map('intval',explode(',',setting('appt_days','1,2,3,4,5,6')));
    $len=max(10,(int)setting('appt_slot','30'));
    [$sh,$sm]=array_map('intval',explode(':',setting('appt_start','10:00')));[$eh,$em]=array_map('intval',explode(':',setting('appt_end','18:00')));
    $busy=[];foreach(DB::all("SELECT start_at,end_at FROM appointments WHERE status IN ('scheduled','completed') AND start_at>=CURDATE() AND start_at<CURDATE()+INTERVAL ? DAY",[$days+1]) as $b)$busy[]=[strtotime($b['start_at']),strtotime($b['end_at'])];
    // Busy times from Google Calendar (when connected) block slots too
    foreach(GCal::busy(strtotime('today'),strtotime('today +'.($days+1).' day')) as $b)$busy[]=$b;
    $out=[];$now=time()+3600;
    for($d=0;$d<=$days;$d++){
      $day=strtotime("today +$d day");
      if(!in_array((int)date('N',$day),$wd,true))continue;
      for($t=$day+$sh*3600+$sm*60;$t+$len*60<=$day+$eh*3600+$em*60;$t+=$len*60){
        if($t<$now)continue;
        $free=true;foreach($busy as [$a,$b])if($t<$b&&$t+$len*60>$a){$free=false;break;}
        if($free)$out[date('Y-m-d',$day)][]=$t;}}
    return $out;}
  static function book($lid,$start,$by='lead',$notes='',$title=null){
    $len=max(10,(int)setting('appt_slot','30'));
    if(DB::val("SELECT id FROM appointments WHERE status='scheduled' AND start_at<? AND end_at>?",[date('Y-m-d H:i:s',$start+$len*60),date('Y-m-d H:i:s',$start)]))throw new Exception('That time was just taken – please pick another slot.');
    $l=DB::row('SELECT * FROM leads WHERE id=?',[$lid]);
    $id=DB::insert('appointments',['lead_id'=>$lid,'title'=>$title?:setting('appt_title','Meeting').' – '.($l['name']??''),'start_at'=>date('Y-m-d H:i:s',$start),'end_at'=>date('Y-m-d H:i:s',$start+$len*60),'location'=>setting('appt_location'),'notes'=>$notes,'booked_by'=>$by,'user_id'=>$_SESSION['uid']??null]);
    GCal::push($id);
    if($l){
      event($lid,'booked','Appointment booked for '.date('D, d M Y · h:i A',$start).($by==='lead'?' (by the lead)':''),'cal');
      if(in_array($l['status'],['new','contacted','qualified'],true))CRM::setStatus($lid,'meeting');
      self::notify($l,$start);
      Automation::fire('appointment_booked',$lid);}
    return $id;}
  private static function notify($l,$start){
    if(!Mailer::ready())return;
    $when=date('l, d M Y \a\t h:i A',$start);$loc=DB::val('SELECT location FROM appointments WHERE lead_id=? AND start_at=? ORDER BY id DESC LIMIT 1',[$l['id'],date('Y-m-d H:i:s',$start)])?:setting('appt_location');
    try{
      if(setting('appt_notify')==='1'&&($to=setting('company_email'))){$t="New appointment booked\n\n{$l['name']}".($l['contact_name']?" ({$l['contact_name']})":'')."\nPhone: {$l['phone']}\nEmail: {$l['email']}\nWhen: $when\n\n".base_url('index.php?r=lead&id='.$l['id']);Mailer::send($to,'📅 New booking: '.$l['name'].' – '.date('d M h:i A',$start),Mailer::html($t),$t);}
      if($l['email']){$t="Hi ".($l['contact_name']?:$l['name']).",\n\nYour ".setting('appt_title','meeting')." with ".setting('company_name')." is confirmed for $when.".($loc?"\nWhere: $loc":'')."\n\nSee you then!\n".setting('sender_name');Outreach::email($l['id'],$l['email'],'Confirmed: '.setting('appt_title','Meeting').' on '.date('d M, h:i A',$start),$t);}}
    catch(Throwable $e){logmsg('Booking mail: '.$e->getMessage());}}
  // Change status (completed / no_show / cancelled); cancelling also removes the Google Calendar event
  static function status($id,$st){
    if(!in_array($st,['scheduled','completed','no_show','cancelled'],true))return;
    $a=DB::row('SELECT * FROM appointments WHERE id=?',[$id]);if(!$a)return;
    DB::update('appointments',['status'=>$st],'id=?',[$id]);
    if($st==='cancelled')GCal::cancel($id);
    if($a['lead_id'])event($a['lead_id'],'note','Appointment marked '.str_replace('_',' ',$st),'cal');}
  // Called by the scheduler: reminders 1 hour before
  static function remind(){
    if(setting('appt_remind')!=='1')return;
    foreach(DB::all("SELECT a.*,l.name,l.contact_name,l.phone,l.email FROM appointments a JOIN leads l ON l.id=a.lead_id WHERE a.status='scheduled' AND a.reminded=0 AND a.start_at BETWEEN NOW() AND NOW()+INTERVAL 70 MINUTE") as $a){
      DB::update('appointments',['reminded'=>1],'id=?',[$a['id']]);
      $t="Reminder: your ".setting('appt_title','meeting')." with ".setting('company_name')." starts at ".date('h:i A',strtotime($a['start_at'])).".".($a['location']?"\n".$a['location']:'');
      try{if($a['phone']&&WhatsApp::provider()!=='manual')WhatsApp::send($a['phone'],$t,[],[]);elseif($a['email']&&Mailer::ready())Mailer::send($a['email'],'Reminder: '.setting('appt_title','Meeting').' today',Mailer::html($t),$t);event($a['lead_id'],'sent','Appointment reminder sent','cal');}
      catch(Throwable $e){logmsg('Reminder: '.$e->getMessage());}}}}
