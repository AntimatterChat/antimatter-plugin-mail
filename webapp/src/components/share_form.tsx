// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

import React, {useMemo, useState} from 'react';
import {useIntl} from 'react-intl';
import {useSelector} from 'react-redux';

import {getCurrentChannelId, getMyChannels} from 'mattermost-redux/selectors/entities/channels';

import {am, own} from '../class_names';
import * as client from '../client';

import {messages} from './messages';

type Props = {
    folder: string;
    uid: number;
    onDone: (channelName: string) => void;
    onCancel: () => void;
};

function channelLabel(channel: {display_name: string; name: string; type: string}) {
    const name = channel.display_name || channel.name;
    return channel.type === 'O' || channel.type === 'P' ? `~${name}` : name;
}

// ShareForm posts a summary of a message into a channel.
export default function ShareForm({folder, uid, onDone, onCancel}: Props) {
    const {formatMessage} = useIntl();
    const myChannels = useSelector(getMyChannels);
    const currentChannelId = useSelector(getCurrentChannelId);
    const channels = useMemo(() => myChannels.
        filter((c) => c.delete_at === 0).
        sort((a, b) => channelLabel(a).localeCompare(channelLabel(b))), [myChannels]);
    const [channelId, setChannelId] = useState(() => (channels.some((c) => c.id === currentChannelId) ? currentChannelId : channels[0]?.id || ''));
    const [comment, setComment] = useState('');
    const [sharing, setSharing] = useState(false);
    const [error, setError] = useState('');

    const share = async (e: React.FormEvent) => {
        e.preventDefault();
        setSharing(true);
        setError('');
        try {
            await client.shareMessage(folder, uid, channelId, comment);
            const channel = channels.find((c) => c.id === channelId);
            onDone(channel ? channelLabel(channel) : '');
        } catch (err) {
            setError((err as Error).message);
            setSharing(false);
        }
    };

    return (
        <form
            className={own('card')}
            onSubmit={share}
        >
            <label
                className={am('field')}
                htmlFor='amm-share-channel'
            >
                <span>{formatMessage(messages.shareChannel)}</span>
                <select
                    id='amm-share-channel'
                    value={channelId}
                    required={true}
                    onChange={(e) => setChannelId(e.target.value)}
                >
                    {channels.map((c) => (
                        <option
                            key={c.id}
                            value={c.id}
                        >
                            {channelLabel(c)}
                        </option>
                    ))}
                </select>
            </label>
            <label
                className={am('field')}
                htmlFor='amm-share-comment'
            >
                <span>{formatMessage(messages.shareComment)}</span>
                <textarea
                    id='amm-share-comment'
                    value={comment}
                    maxLength={4000}
                    onChange={(e) => setComment(e.target.value)}
                />
                <small>{formatMessage(messages.sharePrivacy)}</small>
            </label>
            {error && <div className={own('error')}>{error}</div>}
            <div className={own('actions')}>
                <button
                    type='submit'
                    className={am('btn', 'primary')}
                    disabled={sharing || !channelId}
                >
                    {formatMessage(messages.shareSubmit)}
                </button>
                <button
                    type='button'
                    className={am('btn')}
                    onClick={onCancel}
                >
                    {formatMessage(messages.cancel)}
                </button>
            </div>
        </form>
    );
}
