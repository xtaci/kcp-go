-- create protocol
kcp_protocol = Proto("KCP", "KCP Protocol")

-- fields for kcp
conv = ProtoField.uint32("kcp.conv", "conv", base.DEC)
cmd = ProtoField.uint8("kcp.cmd", "cmd", base.DEC)
frg = ProtoField.uint8("kcp.frg", "frg", base.DEC)
wnd = ProtoField.uint16("kcp.wnd", "wnd", base.DEC)
ts = ProtoField.uint32("kcp.ts", "ts", base.DEC)
sn = ProtoField.uint32("kcp.sn", "sn", base.DEC)
una = ProtoField.uint32("kcp.una", "una", base.DEC)
len = ProtoField.uint32("kcp.len", "len", base.DEC)

kcp_protocol.fields = {conv, cmd, frg, wnd, ts, sn, una, len}

-- dissect each udp packet
function kcp_protocol.dissector(buffer, pinfo, tree)
    length = buffer:len()
    if length == 0 then
        return
    end

    local offset_s = 8
    local first_sn = buffer(offset_s + 12, 4):le_int()
    local first_len = buffer(offset_s + 20, 4):le_int()
    local first_cmd_name = get_cmd_name(buffer(offset_s + 4, 1):le_int())
    local info = string.format("[%s] Sn=%d Kcplen=%d", first_cmd_name, first_sn, first_len)

    pinfo.cols.protocol = kcp_protocol.name
    udp_info = string.gsub(tostring(pinfo.cols.info), "Len", "Udplen", 1)
    pinfo.cols.info = string.gsub(udp_info, " U", info .. " U", 1)

    -- dssect multi kcp packet in udp
    local offset = 8
    while offset < buffer:len() do
        local conv_buf = buffer(offset + 0, 4)
        local cmd_buf = buffer(offset + 4, 1)
        local wnd_buf = buffer(offset + 6, 2)
        local sn_buf = buffer(offset + 12, 4)
        local len_buf = buffer(offset + 20, 4)

        local cmd_name = get_cmd_name(cmd_buf:le_int())
        local data_len = len_buf:le_int()

        local tree_title =
            string.format(
            "KCP Protocol, %s, Sn: %d, Conv: %d, Wnd: %d, Len: %d",
            cmd_name,
            sn_buf:le_int(),
            conv_buf:le_int(),
            wnd_buf:le_int(),
            data_len
        )
        local subtree = tree:add(kcp_protocol, buffer(), tree_title)
        subtree:add_le(conv, conv_buf)
        subtree:add_le(cmd, cmd_buf):append_text(" (" .. cmd_name .. ")")
        subtree:add_le(frg, buffer(offset + 5, 1))
        subtree:add_le(wnd, wnd_buf)
        subtree:add_le(ts, buffer(offset + 8, 4))
        subtree:add_le(sn, sn_buf)
        subtree:add_le(una, buffer(offset + 16, 4))
        subtree:add_le(len, len_buf)
        offset = offset + 24 + data_len
    end
end

function get_cmd_name(cmd_val)
    if cmd_val == 81 then
        return "PSH"
    elseif cmd_val == 82 then
        return "ACK"
    elseif cmd_val == 83 then
        return "ASK"
    elseif cmd_val == 84 then
        return "TELL"
    end
end

-- register kcp dissector to udp
local udp_port = DissectorTable.get("udp.port")
-- replace 8081 to the port for kcp
udp_port:add(8081, kcp_protocol)
