import io
import unittest
import urllib.error
from unittest.mock import patch

import local_events


class RetryTest(unittest.TestCase):
    def test_retry_reuses_event_id_and_body(self):
        attempts = []

        def urlopen(request, timeout):
            attempts.append(request.data)
            if len(attempts) == 1:
                raise urllib.error.HTTPError(
                    request.full_url, 503, "busy", {"Retry-After": "0"}, io.BytesIO()
                )
            return io.BytesIO(b'{"stored":true}')

        with patch.object(local_events.urllib.request, "urlopen", side_effect=urlopen):
            result = local_events.send("span.start", kind="agent", name="job")

        self.assertEqual(result, {"stored": True})
        self.assertEqual(len(attempts), 2)
        self.assertEqual(attempts[0], attempts[1])


if __name__ == "__main__":
    unittest.main()
