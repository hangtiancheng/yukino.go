export interface UserInfo {
  uuid: string;
  nickname: string;
  telephone: string;
  email: string;
  avatar: string;
  gender: number;
  birthday: string;
  signature: string;
  status: number;
  is_admin: number;
  created_at: string;
}

export interface ContactInfo {
  contact_id: string;
  contact_name: string;
  contact_avatar: string;
  contact_phone: string;
  contact_email: string;
  contact_gender: number;
  contact_signature: string;
  contact_birthday: string;
  contact_notice: string;
  contact_members: string[];
  contact_member_cnt: number;
  contact_owner_id: string;
  contact_add_mode: number;
}

export interface Message {
  uuid?: string;
  session_id: string;
  type: number;
  content: string;
  url: string;
  send_id: string;
  send_name: string;
  send_avatar: string;
  receive_id: string;
  file_size: string;
  file_name: string;
  file_type: string;
  created_at: string;
  av_data?: string;
}

export interface AuthResponse {
  token: string;
  user_info: UserInfo;
}

export interface SessionItem {
  user_id?: string;
  username?: string;
  group_id?: string;
  group_name?: string;
  avatar: string;
}

export interface ApiResponse<T = unknown> {
  code: number;
  message: string;
  data: T;
}
